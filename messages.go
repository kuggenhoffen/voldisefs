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
	MsgData		interface{}			// Different message types will be placed here
}

// BaseMessageDecode struct defines a intermediate protocol format that is used to
// decode incoming JSON to recognise MessageType to decode further into correct
// message structs
type BaseMessageDecode struct {
	MsgType		MessageType
	MsgData		*json.RawMessage		// Different message types will be placed here
}

// BootstrapRequest struct defines the bootstrap request message format, where Name
// is the unique name of the requester and ServerPort is the server port of the requester
type BootstrapRequest struct {
	Name			string
	ServerPort		int
}

// BootstrapResponse struct defines the bootstrap response message format, where
// Name is the unique name of the response sender, and Peers is a PeerInfo array of
// known peers to the response sender
type BootstrapResponse struct {
	Name		string
	Peers		[]PeerInfo
}

// ChunkStoreRequest defines the chunk store request message format, the requester
// uses this message to distribute chunks to the network. The message contains 
// ID field of 16 bytes to identify the chunk, and the ChunkData field containing
// the encrypted chunk data
type ChunkStoreRequest ChunkInfo

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
		msg = &BaseMessage{MsgType: bmd.MsgType, MsgData: br}
	case MsgBootstrapResponse:
		br := new(BootstrapResponse)
		err := json.Unmarshal(*bmd.MsgData, br)
		if err != nil {
			InfoLogger.Printf("Decoding error on BootstrapResponse")
			return nil, err
		}
		InfoLogger.Printf("Bootstrap response received with data: %s", bmd.MsgData)
		msg = &BaseMessage{MsgType: bmd.MsgType, MsgData: br}
	}
	
	return msg, nil
	
}

func SendBootstrapRequest(writer io.Writer, ownPeerInfo *PeerInfo) (error) {
	// Get base message struct
	msg := BaseMessage{ MsgType: MsgBootstrapRequest, 
						MsgData: BootstrapRequest{ServerPort: ownPeerInfo.ServerPort, Name: ownPeerInfo.Name} }
	
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
						MsgData: BootstrapResponse{Name: ownPeerInfo.Name, Peers: p} }
	//str,_ := json.Marshal(msg)
	// Create encoder to given writer and encode message
	enc := json.NewEncoder(writer)
	err := enc.Encode(msg)
	
	return err
}

func SendChunkStoreRequest(writer io.Writer, ownPeerInfo *PeerInfo) (error) {
	// Get base message struct
	msg := BaseMessage{ MsgType: MsgBootstrapRequest, 
						MsgData: BootstrapRequest{ServerPort: ownPeerInfo.ServerPort, Name: ownPeerInfo.Name} }
	
	str,_ := json.Marshal(msg)
	InfoLogger.Printf("Sent: %s", str)
	
	// Create encoder to given writer and encode message
	enc := json.NewEncoder(writer)
	err := enc.Encode(msg)
	
	if err == nil {
		InfoLogger.Printf("Sent bootstrap request message")
	}
	
	return err
}
