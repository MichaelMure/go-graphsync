package responsebuilder

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/ipfs/go-graphsync"
	gsmsg "github.com/ipfs/go-graphsync/message"
	"github.com/ipfs/go-graphsync/metadata"
	"github.com/ipfs/go-graphsync/testutil"
	"github.com/ipld/go-ipld-prime"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/stretchr/testify/require"
)

func TestMessageBuilding(t *testing.T) {
	rb := New()
	blocks := testutil.GenerateBlocksOfSize(3, 100)
	links := make([]ipld.Link, 0, len(blocks))
	for _, block := range blocks {
		links = append(links, cidlink.Link{Cid: block.Cid()})
	}
	requestID1 := graphsync.RequestID(rand.Int31())
	requestID2 := graphsync.RequestID(rand.Int31())
	requestID3 := graphsync.RequestID(rand.Int31())
	requestID4 := graphsync.RequestID(rand.Int31())

	rb.AddLink(requestID1, links[0], true)
	rb.AddLink(requestID1, links[1], false)
	rb.AddLink(requestID1, links[2], true)

	rb.AddCompletedRequest(requestID1, graphsync.RequestCompletedPartial)

	rb.AddLink(requestID2, links[1], true)
	rb.AddLink(requestID2, links[2], true)
	rb.AddLink(requestID2, links[1], true)

	rb.AddCompletedRequest(requestID2, graphsync.RequestCompletedFull)

	rb.AddLink(requestID3, links[0], true)
	rb.AddLink(requestID3, links[1], true)

	rb.AddCompletedRequest(requestID4, graphsync.RequestCompletedFull)

	for _, block := range blocks {
		rb.AddBlock(block)
	}

	require.Equal(t, rb.BlockSize(), 300, "did not calculate block size correctly")

	extensionData1 := testutil.RandomBytes(100)
	extensionName1 := graphsync.ExtensionName("AppleSauce/McGee")
	extension1 := graphsync.ExtensionData{
		Name: extensionName1,
		Data: extensionData1,
	}
	extensionData2 := testutil.RandomBytes(100)
	extensionName2 := graphsync.ExtensionName("HappyLand/Happenstance")
	extension2 := graphsync.ExtensionData{
		Name: extensionName2,
		Data: extensionData2,
	}
	rb.AddExtensionData(requestID1, extension1)
	rb.AddExtensionData(requestID3, extension2)

	responses, sentBlocks, err := rb.Build()

	require.NoError(t, err, "build responses errored")

	require.Len(t, responses, 4, "did not assemble correct number of responses")

	response1, err := findResponseForRequestID(responses, requestID1)
	require.NoError(t, err)
	require.Equal(t, response1.Status(), graphsync.RequestCompletedPartial, "did not generate completed partial response")

	response1MetadataRaw, found := response1.Extension(graphsync.ExtensionMetadata)
	require.True(t, found, "Metadata should be included in response")
	response1Metadata, err := metadata.DecodeMetadata(response1MetadataRaw)
	require.NoError(t, err)
	require.Equal(t, response1Metadata, metadata.Metadata{
		metadata.Item{Link: links[0], BlockPresent: true},
		metadata.Item{Link: links[1], BlockPresent: false},
		metadata.Item{Link: links[2], BlockPresent: true},
	}, "incorrect metadata included in response")

	response1ReturnedExtensionData, found := response1.Extension(extensionName1)
	require.True(t, found)
	require.Equal(t, extensionData1, response1ReturnedExtensionData, "did not encode first extension")

	response2, err := findResponseForRequestID(responses, requestID2)
	require.NoError(t, err)
	require.Equal(t, response2.Status(), graphsync.RequestCompletedFull, "did not generate completed full response")

	response2MetadataRaw, found := response2.Extension(graphsync.ExtensionMetadata)
	require.True(t, found, "Metadata should be included in response")
	response2Metadata, err := metadata.DecodeMetadata(response2MetadataRaw)
	require.NoError(t, err)
	require.Equal(t, response2Metadata, metadata.Metadata{
		metadata.Item{Link: links[1], BlockPresent: true},
		metadata.Item{Link: links[2], BlockPresent: true},
		metadata.Item{Link: links[1], BlockPresent: true},
	}, "incorrect metadata included in response")

	response3, err := findResponseForRequestID(responses, requestID3)
	require.NoError(t, err)
	require.Equal(t, response3.Status(), graphsync.PartialResponse, "did not generate partial response")

	response3MetadataRaw, found := response3.Extension(graphsync.ExtensionMetadata)
	require.True(t, found, "Metadata should be included in response")
	response3Metadata, err := metadata.DecodeMetadata(response3MetadataRaw)
	require.NoError(t, err)
	require.Equal(t, response3Metadata, metadata.Metadata{
		metadata.Item{Link: links[0], BlockPresent: true},
		metadata.Item{Link: links[1], BlockPresent: true},
	}, "incorrect metadata included in response")

	response3ReturnedExtensionData, found := response3.Extension(extensionName2)
	require.True(t, found)
	require.Equal(t, extensionData2, response3ReturnedExtensionData, "did not encode second extension")

	response4, err := findResponseForRequestID(responses, requestID4)
	require.NoError(t, err)
	require.Equal(t, response4.Status(), graphsync.RequestCompletedFull, "did not generate completed full response")

	require.Equal(t, len(sentBlocks), len(blocks), "did not send all blocks")

	for _, block := range sentBlocks {
		testutil.AssertContainsBlock(t, blocks, block)
	}
}

func findResponseForRequestID(responses []gsmsg.GraphSyncResponse, requestID graphsync.RequestID) (gsmsg.GraphSyncResponse, error) {
	for _, response := range responses {
		if response.RequestID() == requestID {
			return response, nil
		}
	}
	return gsmsg.GraphSyncResponse{}, fmt.Errorf("Response Not Found")
}
