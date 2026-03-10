package endpoints

import (
	"github.com/prebid/openrtb/v20/openrtb2"
	"github.com/prebid/prebid-server/v3/partners"
)

// AppendSChain injects the exchange's SupplyChain node into the bid request.
// It ensures thread-safety by deep-cloning any shared objects that it modifies.
func AppendSChain(req *openrtb2.BidRequest, ssp partners.SSPInventory, globalASI string) {
	if req == nil {
		return
	}

	// Determine ASI: Use SSP specific node, then global config, then fallback
	asi := ssp.SChainNode
	if asi == "" {
		asi = globalASI
	}
	if asi == "" {
		asi = "my-ad-exchange.com"
	}

	// 1. Deep clone the Source object
	var newSource *openrtb2.Source
	if req.Source != nil {
		srcCopy := *req.Source
		newSource = &srcCopy
	} else {
		newSource = &openrtb2.Source{}
	}
	req.Source = newSource

	// 2. Prepare the new node for this exchange
	hpValue := int8(1)
	newNode := openrtb2.SupplyChainNode{
		ASI: asi,               // Exchange Domain
		SID: ssp.SSPIdentifier, // This exchange's ID on the upstream side
		RID: req.ID,            // Current Bid Request ID
		HP:  &hpValue,
	}

	// 3. Clone and append to the SChain object
	if newSource.SChain == nil {
		newSource.SChain = &openrtb2.SupplyChain{
			Ver:      "1.0",
			Complete: 1,
			Nodes:    []openrtb2.SupplyChainNode{newNode},
		}
	} else {
		// Deep clone SChain structure and its slice of Nodes
		scCopy := *newSource.SChain
		newSource.SChain = &scCopy

		newNodes := make([]openrtb2.SupplyChainNode, len(scCopy.Nodes)+1)
		copy(newNodes, scCopy.Nodes)
		newNodes[len(scCopy.Nodes)] = newNode
		newSource.SChain.Nodes = newNodes
	}
}
