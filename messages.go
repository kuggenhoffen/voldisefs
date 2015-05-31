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

type MessageInfo struct {
	Src		*PeerInfo
	Data	*BaseMessage
}

type BaseMessage struct {
	MsgType		MessageType
	MsgData		interface{}			// Different message types will be placed here
}

type BaseMessageDecode struct {
	MsgType		MessageType
	MsgData		*json.RawMessage		// Different message types will be placed here
}

type PeerInfo struct {
	Name			string
	Address			string
	ServerPort		int
}

func (pi *PeerInfo) String() string {
	return fmt.Sprintf("[Name: %s, Address: %s:%d]", pi.Name, pi.Address, pi.ServerPort)
}

func (pi *PeerInfo) ToKey() string {
	return fmt.Sprintf("%s:%d", pi.Address, pi.ServerPort)
}

type BootstrapRequest struct {
	Name			string
	ServerPort		int
}

type BootstrapResponse struct {
	Name		string
	Peers		[]PeerInfo
}

func DecodeMessage(reader io.Reader) (*BaseMessage, error) {
	InfoLogger.Printf("Decoding input")
	// Get JSON decoder using given reader
	dec := bufio.NewReader(reader)
	data, err := dec.ReadString('\n')
	if err != nil {
		InfoLogger.Printf("Reading error on BaseMessage")
		return nil, err
	}
	InfoLogger.Printf("Data: %s", data)
	
	// First decode into BaseMessageDecode, so that the payload stays encoded
	bmd := new(BaseMessageDecode)
	err = json.Unmarshal([]byte(data), bmd)
	InfoLogger.Printf("Data: %s", bmd.MsgData)
	//err := dec.Decode(bmd)
	
	if err != nil {
		InfoLogger.Printf("Decoding error on BaseMessage")
		return nil, err
	}
	
	msg := new(BaseMessage)
	
	switch bmd.MsgType {
	case MsgBootstrapRequest:
		br := new(BootstrapRequest)
		InfoLogger.Printf("Data: %s", bmd.MsgData)
		//err := json.Unmarshal(bmd.MsgData, br)
		if err != nil {
			InfoLogger.Printf("Decoding error on BootstrapRequest")
			return nil, err
		}
		msg = &BaseMessage{MsgType: MsgBootstrapRequest, MsgData: br}
	case MsgBootstrapResponse:
		br := new(BootstrapResponse)
		//err := json.Unmarshal(bmd.MsgData, br)
		if err != nil {
			InfoLogger.Printf("Decoding error on BootstrapRequest")
			return nil, err
		}
		msg = &BaseMessage{MsgType: MsgBootstrapRequest, MsgData: br}
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
	
	if err == nil {
		InfoLogger.Printf("Sent bootstrap request message")
	}
	
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
	msg := BaseMessage{ MsgType: MsgBootstrapRequest, 
						MsgData: BootstrapResponse{Name: ownPeerInfo.Name, Peers: p} }
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