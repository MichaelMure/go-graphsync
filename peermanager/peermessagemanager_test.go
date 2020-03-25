package peermanager

import (
	"context"
	"math/rand"
	"testing"
	"time"

	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-graphsync"
	gsmsg "github.com/ipfs/go-graphsync/message"
	"github.com/ipfs/go-graphsync/testutil"
	ipldfree "github.com/ipld/go-ipld-prime/impl/free"
	"github.com/ipld/go-ipld-prime/traversal/selector/builder"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/stretchr/testify/require"
)

type messageSent struct {
	p       peer.ID
	message gsmsg.GraphSyncMessage
}

type fakePeer struct {
	p            peer.ID
	messagesSent chan messageSent
}

func (fp *fakePeer) Startup()  {}
func (fp *fakePeer) Shutdown() {}

func (fp *fakePeer) AddRequest(graphSyncRequest gsmsg.GraphSyncRequest) {
	message := gsmsg.New()
	message.AddRequest(graphSyncRequest)
	fp.messagesSent <- messageSent{fp.p, message}
}

func (fp *fakePeer) AddResponses([]gsmsg.GraphSyncResponse, []blocks.Block) <-chan struct{} {
	return nil
}

func makePeerQueueFactory(messagesSent chan messageSent) PeerQueueFactory {
	return func(ctx context.Context, p peer.ID) PeerQueue {
		return &fakePeer{
			p:            p,
			messagesSent: messagesSent,
		}
	}
}

func TestSendingMessagesToPeers(t *testing.T) {
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	messagesSent := make(chan messageSent, 5)
	peerQueueFactory := makePeerQueueFactory(messagesSent)

	tp := testutil.GeneratePeers(5)

	id := graphsync.RequestID(rand.Int31())
	priority := graphsync.Priority(rand.Int31())
	root := testutil.GenerateCids(1)[0]
	ssb := builder.NewSelectorSpecBuilder(ipldfree.NodeBuilder())
	selector := ssb.Matcher().Node()

	peerManager := NewMessageManager(ctx, peerQueueFactory)

	request := gsmsg.NewRequest(id, root, selector, priority)
	peerManager.SendRequest(tp[0], request)
	peerManager.SendRequest(tp[1], request)
	cancelRequest := gsmsg.CancelRequest(id)
	peerManager.SendRequest(tp[0], cancelRequest)

	var firstMessage messageSent
	testutil.AssertReceive(ctx, t, messagesSent, &firstMessage, "first message sent")
	require.Equal(t, firstMessage.p, tp[0], "First message sent to wrong peer")
	request = firstMessage.message.Requests()[0]
	require.Equal(t, request.ID(), id)
	require.False(t, request.IsCancel())
	require.Equal(t, request.Priority(), priority)
	require.Equal(t, request.Selector(), selector)

	var secondMessage messageSent
	testutil.AssertReceive(ctx, t, messagesSent, &secondMessage, "first message sent")
	require.Equal(t, secondMessage.p, tp[1], "Second message sent to correct peer")
	request = secondMessage.message.Requests()[0]
	require.Equal(t, request.ID(), id)
	require.False(t, request.IsCancel())
	require.Equal(t, request.Priority(), priority)
	require.Equal(t, request.Selector(), selector)

	var thirdMessage messageSent
	testutil.AssertReceive(ctx, t, messagesSent, &thirdMessage, "first message sent")

	require.Equal(t, thirdMessage.p, tp[0], "Third message sent to wrong peer")
	request = thirdMessage.message.Requests()[0]
	require.Equal(t, request.ID(), id)
	require.True(t, request.IsCancel())

	connectedPeers := peerManager.ConnectedPeers()
	require.Len(t, connectedPeers, 2)

	testutil.AssertContainsPeer(t, connectedPeers, tp[0])
	testutil.AssertContainsPeer(t, connectedPeers, tp[1])
}
