package main

import (
	"encoding/json"
	"io"
	"fmt"
	"bufio"
)

type MessageType int
const (
	MsgBootstrapRequest MessageType = iota
	MsgBootstrapResponse
	MsgChunkStoreRequest
	MsgChunkStoreResponse
	MsgChunkRetrieveRequest
	MsgChunkRetrieveResponse
)

type PeerState int
const (
	StateBootstrapStart	PeerState = iota
	StateBootstrapWait
	StateIdle
)

type MessageInfo struct {
	Src		*PeerInfo
	Data	*BaseMessage
}

// BaseMessage struct defines the protocol format that gets encoded into JSON and 
// is used for communicating between nodes
type BaseMessage struct {
	MsgType		MessageType
	Name		string
	MsgData		interface{}			// Different message types will be placed here
}

// BaseMessageDecode struct defines a intermediate protocol format that is used to
// decode incoming JSON to recognise MessageType to decode further into correct
// message structs
type BaseMessageDecode struct {
	MsgType		MessageType
	Name		string
	MsgData		*json.RawMessage		// Different message types will be placed here
}

// BootstrapRequest struct defines the bootstrap request message format, where Name
// is the unique name of the requester and ServerPort is the server port of the requester
type BootstrapRequest struct {
	ServerPort		int
}

// BootstrapResponse struct defines the bootstrap response message format, where
// Name is the unique name of the response sender, and Peers is a PeerInfo array of
// known peers to the response sender
type BootstrapResponse struct {
	Peers		[]PeerInfo
}

// ChunkStoreRequest defines the chunk store request message format, the requester
// uses this message to distribute chunks to the network. The message contains 
// ID field of 16 bytes to identify the chunk, and the ChunkData field containing
// the encrypted chunk data
type ChunkStoreRequest ChunkInfo

// ChunkRetrieveRequest defines the chunk retrieval message format, the requester
// uses this message to request chunks by their id from a peer.
type ChunkRetrieveRequest struct {
	ID		ChunkID
}

// ChunkRetrieveResponse defines the chunk retrieval response message format
// containing the chunk id and data if one was found, otherwise both fields 
// will be empty
type ChunkRetrieveResponse ChunkInfo

type PeerInfo struct {
	Name			string
	Address			string
	ServerPort		int
	State	PeerState
}

func (pi *PeerInfo) String() string {
	return fmt.Sprintf("[Name: %s, Address: %s:%d]", pi.Name, pi.Address, pi.ServerPort)
}

func (pi *PeerInfo) ToKey() string {
	return fmt.Sprintf("%s:%d", pi.Address, pi.ServerPort)
}


func DecodeMessage(reader io.Reader) (*BaseMessage, error) {
	// Get JSON decoder using given reader
	dec := json.NewDecoder(bufio.NewReader(reader))

	// First decode into BaseMessageDecode, so that the payload stays encoded
	bmd := new(BaseMessageDecode)
	err := dec.Decode(bmd)
	
	if err != nil {
		InfoLogger.Printf("Decoding error on BaseMessage")
		return nil, err
	}
	
	// Initialize BaseMessage struct and decode MsgData field based on MsgType field
	msg := new(BaseMessage)
	switch bmd.MsgType {
	case MsgBootstrapRequest:
		br := new(BootstrapRequest)
		err := json.Unmarshal(*bmd.MsgData, br)
		if err != nil {
			InfoLogger.Printf("Decoding error on BootstrapRequest")
			return nil, err
		}
		InfoLogger.Printf("Bootstrap request received with data: %s", bmd.MsgData)
		msg = &BaseMessage{MsgType: bmd.MsgType, Name: bmd.Name, MsgData: br}
	case MsgBootstrapResponse:
		br := new(BootstrapResponse)
		err := json.Unmarshal(*bmd.MsgData, br)
		if err != nil {
			InfoLogger.Printf("Decoding error on BootstrapResponse")
			return nil, err
		}
		InfoLogger.Printf("Bootstrap response received with data: %s", bmd.MsgData)
		msg = &BaseMessage{MsgType: bmd.MsgType, Name: bmd.Name, MsgData: br}
	case MsgChunkStoreRequest:
		ci := new(ChunkInfo)
		err := json.Unmarshal(*bmd.MsgData, ci)
		if err != nil {
			InfoLogger.Printf("Decoding error on ChunkStoreRequest")
			return nil, err
		}
		InfoLogger.Printf("Chunk store request received with data: %s", bmd.MsgData)
		msg = &BaseMessage{MsgType: bmd.MsgType, Name: bmd.Name, MsgData: ci}
	case MsgChunkRetrieveRequest:
		cr := new(ChunkRetrieveRequest)
		err := json.Unmarshal(*bmd.MsgData, cr)
		if err != nil {
			InfoLogger.Printf("Decoding error on ChunkRetrieveRequest")
			return nil, err
		}
		InfoLogger.Printf("Chunk retrieve request received with data: %s", bmd.MsgData)
		msg = &BaseMessage{MsgType: bmd.MsgType, Name: bmd.Name, MsgData: cr}
	case MsgChunkRetrieveResponse:
		cr := new(ChunkRetrieveResponse)
		err := json.Unmarshal(*bmd.MsgData, cr)
		if err != nil {
			InfoLogger.Printf("Decoding error on ChunkRetrieveResponse")
			return nil, err
		}
		InfoLogger.Printf("Chunk retrieve response received with data: %s", bmd.MsgData)
		msg = &BaseMessage{MsgType: bmd.MsgType, Name: bmd.Name, MsgData: cr}
	}
	
	return msg, nil
	
}

func SendBootstrapRequest(writer io.Writer, ownPeerInfo *PeerInfo) (error) {
	// Get base message struct
	msg := BaseMessage{ MsgType: MsgBootstrapRequest, 
						Name: ownPeerInfo.Name,
						MsgData: BootstrapRequest{ServerPort: ownPeerInfo.ServerPort} }
	
	str,_ := json.Marshal(msg)
	InfoLogger.Printf("Sent: %s", str)
	
	// Create encoder to given writer and encode message
	enc := json.NewEncoder(writer)
	err := enc.Encode(msg)
	
	return err
}

func SendBootstrapResponse(writer io.Writer, ownPeerInfo *PeerInfo, peerInfos PeerManager) (error) {
	// Claim read mutex and generate MessageServerInfo array containng all peer's info
	peerInfos.RLock()
	defer peerInfos.RUnlock()
	
	p := make([]PeerInfo, len(peerInfos.peers))
	i := 0
	for _, peer := range peerInfos.peers {
		p[i] = *peer
		i = i + 1
	}
	
	// Create bootstrap response message structure
	msg := BaseMessage{ MsgType: MsgBootstrapResponse, 
						Name: ownPeerInfo.Name,
						MsgData: BootstrapResponse{Peers: p} }
	//str,_ := json.Marshal(msg)
	// Create encoder to given writer and encode message
	enc := json.NewEncoder(writer)
	err := enc.Encode(msg)
	
	return err
}

func SendChunkStoreRequest(recv *PeerInfo, ch ChunkInfo) (error) {
	c, e := GetClientConnection(recv)
	if e != nil {
		return e
	}
	
	// Get base message struct
	msg := BaseMessage{ MsgType: MsgChunkStoreRequest, 
						Name: ownInfo.Name,
						MsgData: ch }
	
	str,_ := json.Marshal(msg)
	InfoLogger.Printf("Sent: %s", str)
	
	// Create encoder to given connection and encode message
	enc := json.NewEncoder(c)
	err := enc.Encode(msg)
	
	return err
}

func SendChunkRetrieveRequest(recv *PeerInfo, cid ChunkID) (error) {
	c, e := GetClientConnection(recv)
	if e != nil {
		return e
	}
	
	// Get base message struct
	msg := BaseMessage{ MsgType: MsgChunkRetrieveRequest, 
						Name: ownInfo.Name,
						MsgData: ChunkRetrieveRequest{ ID: cid } }
	
	str,_ := json.Marshal(msg)
	InfoLogger.Printf("Sent: %s", str)
	
	// Create encoder to given connection and encode message
	enc := json.NewEncoder(c)
	err := enc.Encode(msg)
	
	return err
}

func SendChunkRetrieveResponse(w io.Writer, recv *PeerInfo, ci *ChunkInfo) (error) {
	// Get base message struct
	msg := BaseMessage{ MsgType: MsgChunkRetrieveResponse, 
						Name: ownInfo.Name,
						MsgData: ci }
	
	str,_ := json.Marshal(msg)
	InfoLogger.Printf("Sent: %s", str)
	
	// Create encoder to given connection and encode message
	enc := json.NewEncoder(w)
	err := enc.Encode(msg)
	
	return err
}
