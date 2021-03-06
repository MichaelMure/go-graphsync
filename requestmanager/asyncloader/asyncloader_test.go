package asyncloader

import (
	"context"
	"io"
	"math/rand"
	"testing"
	"time"

	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-graphsync"
	"github.com/ipfs/go-graphsync/metadata"
	"github.com/ipfs/go-graphsync/requestmanager/types"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/stretchr/testify/require"

	"github.com/ipfs/go-graphsync/testutil"
	ipld "github.com/ipld/go-ipld-prime"
)

func TestAsyncLoadInitialLoadSucceedsLocallyPresent(t *testing.T) {
	block := testutil.GenerateBlocksOfSize(1, 100)[0]
	st := newStore()
	link := st.Store(t, block)
	withLoader(st, func(ctx context.Context, asyncLoader *AsyncLoader) {
		requestID := graphsync.RequestID(rand.Int31())
		resultChan := asyncLoader.AsyncLoad(requestID, link)
		assertSuccessResponse(ctx, t, resultChan)
		st.AssertLocalLoads(t, 1)
	})
}

func TestAsyncLoadInitialLoadSucceedsResponsePresent(t *testing.T) {
	blocks := testutil.GenerateBlocksOfSize(1, 100)
	block := blocks[0]
	link := cidlink.Link{Cid: block.Cid()}

	st := newStore()
	withLoader(st, func(ctx context.Context, asyncLoader *AsyncLoader) {
		requestID := graphsync.RequestID(rand.Int31())
		responses := map[graphsync.RequestID]metadata.Metadata{
			requestID: metadata.Metadata{
				metadata.Item{
					Link:         link,
					BlockPresent: true,
				},
			},
		}
		asyncLoader.ProcessResponse(responses, blocks)
		resultChan := asyncLoader.AsyncLoad(requestID, link)

		assertSuccessResponse(ctx, t, resultChan)
		st.AssertLocalLoads(t, 0)
		st.AssertBlockStored(t, block)
	})
}

func TestAsyncLoadInitialLoadFails(t *testing.T) {
	st := newStore()
	withLoader(st, func(ctx context.Context, asyncLoader *AsyncLoader) {
		link := testutil.NewTestLink()
		requestID := graphsync.RequestID(rand.Int31())

		responses := map[graphsync.RequestID]metadata.Metadata{
			requestID: metadata.Metadata{
				metadata.Item{
					Link:         link,
					BlockPresent: false,
				},
			},
		}
		asyncLoader.ProcessResponse(responses, nil)

		resultChan := asyncLoader.AsyncLoad(requestID, link)
		assertFailResponse(ctx, t, resultChan)
		st.AssertLocalLoads(t, 0)
	})
}

func TestAsyncLoadInitialLoadIndeterminateWhenRequestNotInProgress(t *testing.T) {
	st := newStore()
	withLoader(st, func(ctx context.Context, asyncLoader *AsyncLoader) {
		link := testutil.NewTestLink()
		requestID := graphsync.RequestID(rand.Int31())
		resultChan := asyncLoader.AsyncLoad(requestID, link)
		assertFailResponse(ctx, t, resultChan)
		st.AssertLocalLoads(t, 1)
	})
}

func TestAsyncLoadInitialLoadIndeterminateThenSucceeds(t *testing.T) {
	blocks := testutil.GenerateBlocksOfSize(1, 100)
	block := blocks[0]
	link := cidlink.Link{Cid: block.Cid()}

	st := newStore()

	withLoader(st, func(ctx context.Context, asyncLoader *AsyncLoader) {
		requestID := graphsync.RequestID(rand.Int31())
		err := asyncLoader.StartRequest(requestID, "")
		require.NoError(t, err)
		resultChan := asyncLoader.AsyncLoad(requestID, link)

		st.AssertAttemptLoadWithoutResult(ctx, t, resultChan)

		responses := map[graphsync.RequestID]metadata.Metadata{
			requestID: metadata.Metadata{
				metadata.Item{
					Link:         link,
					BlockPresent: true,
				},
			},
		}
		asyncLoader.ProcessResponse(responses, blocks)
		assertSuccessResponse(ctx, t, resultChan)
		st.AssertLocalLoads(t, 1)
		st.AssertBlockStored(t, block)
	})
}

func TestAsyncLoadInitialLoadIndeterminateThenFails(t *testing.T) {
	st := newStore()

	withLoader(st, func(ctx context.Context, asyncLoader *AsyncLoader) {
		link := testutil.NewTestLink()
		requestID := graphsync.RequestID(rand.Int31())
		err := asyncLoader.StartRequest(requestID, "")
		require.NoError(t, err)
		resultChan := asyncLoader.AsyncLoad(requestID, link)

		st.AssertAttemptLoadWithoutResult(ctx, t, resultChan)

		responses := map[graphsync.RequestID]metadata.Metadata{
			requestID: metadata.Metadata{
				metadata.Item{
					Link:         link,
					BlockPresent: false,
				},
			},
		}
		asyncLoader.ProcessResponse(responses, nil)
		assertFailResponse(ctx, t, resultChan)
		st.AssertLocalLoads(t, 1)
	})
}

func TestAsyncLoadInitialLoadIndeterminateThenRequestFinishes(t *testing.T) {
	st := newStore()
	withLoader(st, func(ctx context.Context, asyncLoader *AsyncLoader) {
		link := testutil.NewTestLink()
		requestID := graphsync.RequestID(rand.Int31())
		err := asyncLoader.StartRequest(requestID, "")
		require.NoError(t, err)
		resultChan := asyncLoader.AsyncLoad(requestID, link)
		st.AssertAttemptLoadWithoutResult(ctx, t, resultChan)
		asyncLoader.CompleteResponsesFor(requestID)
		assertFailResponse(ctx, t, resultChan)
		st.AssertLocalLoads(t, 1)
	})
}

func TestAsyncLoadTwiceLoadsLocallySecondTime(t *testing.T) {
	blocks := testutil.GenerateBlocksOfSize(1, 100)
	block := blocks[0]
	link := cidlink.Link{Cid: block.Cid()}
	st := newStore()
	withLoader(st, func(ctx context.Context, asyncLoader *AsyncLoader) {
		requestID := graphsync.RequestID(rand.Int31())
		responses := map[graphsync.RequestID]metadata.Metadata{
			requestID: metadata.Metadata{
				metadata.Item{
					Link:         link,
					BlockPresent: true,
				},
			},
		}
		asyncLoader.ProcessResponse(responses, blocks)
		resultChan := asyncLoader.AsyncLoad(requestID, link)

		assertSuccessResponse(ctx, t, resultChan)
		st.AssertLocalLoads(t, 0)

		resultChan = asyncLoader.AsyncLoad(requestID, link)
		assertSuccessResponse(ctx, t, resultChan)
		st.AssertLocalLoads(t, 1)

		st.AssertBlockStored(t, block)
	})
}

func TestRequestSplittingLoadLocallyFromBlockstore(t *testing.T) {
	st := newStore()
	otherSt := newStore()
	block := testutil.GenerateBlocksOfSize(1, 100)[0]
	link := otherSt.Store(t, block)
	withLoader(st, func(ctx context.Context, asyncLoader *AsyncLoader) {
		err := asyncLoader.RegisterPersistenceOption("other", otherSt.loader, otherSt.storer)
		require.NoError(t, err)
		requestID1 := graphsync.RequestID(rand.Int31())
		resultChan1 := asyncLoader.AsyncLoad(requestID1, link)
		requestID2 := graphsync.RequestID(rand.Int31())
		err = asyncLoader.StartRequest(requestID2, "other")
		require.NoError(t, err)
		resultChan2 := asyncLoader.AsyncLoad(requestID2, link)

		assertFailResponse(ctx, t, resultChan1)
		assertSuccessResponse(ctx, t, resultChan2)
		st.AssertLocalLoads(t, 1)
	})
}

func TestRequestSplittingSameBlockTwoStores(t *testing.T) {
	st := newStore()
	otherSt := newStore()
	blocks := testutil.GenerateBlocksOfSize(1, 100)
	block := blocks[0]
	link := cidlink.Link{Cid: block.Cid()}
	withLoader(st, func(ctx context.Context, asyncLoader *AsyncLoader) {
		err := asyncLoader.RegisterPersistenceOption("other", otherSt.loader, otherSt.storer)
		require.NoError(t, err)
		requestID1 := graphsync.RequestID(rand.Int31())
		requestID2 := graphsync.RequestID(rand.Int31())
		err = asyncLoader.StartRequest(requestID1, "")
		require.NoError(t, err)
		err = asyncLoader.StartRequest(requestID2, "other")
		require.NoError(t, err)
		resultChan1 := asyncLoader.AsyncLoad(requestID1, link)
		resultChan2 := asyncLoader.AsyncLoad(requestID2, link)
		responses := map[graphsync.RequestID]metadata.Metadata{
			requestID1: metadata.Metadata{
				metadata.Item{
					Link:         link,
					BlockPresent: true,
				},
			},
			requestID2: metadata.Metadata{
				metadata.Item{
					Link:         link,
					BlockPresent: true,
				},
			},
		}
		asyncLoader.ProcessResponse(responses, blocks)

		assertSuccessResponse(ctx, t, resultChan1)
		assertSuccessResponse(ctx, t, resultChan2)
		st.AssertBlockStored(t, block)
		otherSt.AssertBlockStored(t, block)
	})
}

func TestRequestSplittingSameBlockOnlyOneResponse(t *testing.T) {
	st := newStore()
	otherSt := newStore()
	blocks := testutil.GenerateBlocksOfSize(1, 100)
	block := blocks[0]
	link := cidlink.Link{Cid: block.Cid()}
	withLoader(st, func(ctx context.Context, asyncLoader *AsyncLoader) {
		err := asyncLoader.RegisterPersistenceOption("other", otherSt.loader, otherSt.storer)
		require.NoError(t, err)
		requestID1 := graphsync.RequestID(rand.Int31())
		requestID2 := graphsync.RequestID(rand.Int31())
		err = asyncLoader.StartRequest(requestID1, "")
		require.NoError(t, err)
		err = asyncLoader.StartRequest(requestID2, "other")
		require.NoError(t, err)
		resultChan1 := asyncLoader.AsyncLoad(requestID1, link)
		resultChan2 := asyncLoader.AsyncLoad(requestID2, link)
		responses := map[graphsync.RequestID]metadata.Metadata{
			requestID2: metadata.Metadata{
				metadata.Item{
					Link:         link,
					BlockPresent: true,
				},
			},
		}
		asyncLoader.ProcessResponse(responses, blocks)
		asyncLoader.CompleteResponsesFor(requestID1)

		assertFailResponse(ctx, t, resultChan1)
		assertSuccessResponse(ctx, t, resultChan2)
		otherSt.AssertBlockStored(t, block)
	})
}

type store struct {
	internalLoader ipld.Loader
	storer         ipld.Storer
	blockstore     map[ipld.Link][]byte
	localLoads     int
	called         chan struct{}
}

func newStore() *store {
	blockstore := make(map[ipld.Link][]byte)
	loader, storer := testutil.NewTestStore(blockstore)
	return &store{
		internalLoader: loader,
		storer:         storer,
		blockstore:     blockstore,
		localLoads:     0,
		called:         make(chan struct{}),
	}
}

func (st *store) loader(lnk ipld.Link, lnkCtx ipld.LinkContext) (io.Reader, error) {
	select {
	case <-st.called:
	default:
		close(st.called)
	}
	st.localLoads++
	return st.internalLoader(lnk, lnkCtx)
}

func (st *store) AssertLocalLoads(t *testing.T, localLoads int) {
	require.Equalf(t, localLoads, st.localLoads, "should have loaded locally %d times", localLoads)
}

func (st *store) AssertBlockStored(t *testing.T, blk blocks.Block) {
	require.Equal(t, blk.RawData(), st.blockstore[cidlink.Link{Cid: blk.Cid()}], "should store block")
}

func (st *store) AssertAttemptLoadWithoutResult(ctx context.Context, t *testing.T, resultChan <-chan types.AsyncLoadResult) {
	testutil.AssertDoesReceiveFirst(t, st.called, "should attempt load with no result", resultChan, ctx.Done())
}

func (st *store) Store(t *testing.T, blk blocks.Block) ipld.Link {
	writer, commit, err := st.storer(ipld.LinkContext{})
	require.NoError(t, err)
	_, err = writer.Write(blk.RawData())
	require.NoError(t, err, "seeds block store")
	link := cidlink.Link{Cid: blk.Cid()}
	err = commit(link)
	require.NoError(t, err, "seeds block store")
	return link
}

func withLoader(st *store, exec func(ctx context.Context, asyncLoader *AsyncLoader)) {
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	asyncLoader := New(ctx, st.loader, st.storer)
	asyncLoader.Startup()
	exec(ctx, asyncLoader)
}

func assertSuccessResponse(ctx context.Context, t *testing.T, resultChan <-chan types.AsyncLoadResult) {
	var result types.AsyncLoadResult
	testutil.AssertReceive(ctx, t, resultChan, &result, "should close response channel with response")
	require.NotNil(t, result.Data, "should send response")
	require.Nil(t, result.Err, "should not send error")
}

func assertFailResponse(ctx context.Context, t *testing.T, resultChan <-chan types.AsyncLoadResult) {
	var result types.AsyncLoadResult
	testutil.AssertReceive(ctx, t, resultChan, &result, "should close response channel with response")
	require.Nil(t, result.Data, "should not send responses")
	require.NotNil(t, result.Err, "should send an error")
}
