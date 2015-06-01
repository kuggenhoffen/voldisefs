package main

import (
	"net"
	"fmt"
	"sync"
	"code.google.com/p/go-uuid/uuid"
)

type PeerMap map[string]*PeerInfo

// PeerManager struct for managing a map of peers where keys are the
// remote ip:port as a string and values are a pointer to a Peer struct
// instantiated in PeerServerConnection function 
type PeerManager struct {
	sync.RWMutex
	peers 			PeerMap
}

var (
	// Peer list for storing info about each connected peer
	peerManager 			PeerManager
	// Own local address information
	ownInfo					PeerInfo					
)

// Convenience method for concurrency safe addition of new peers to the map
func (pm *PeerManager) addPeer(newpeer *PeerInfo) {
	pm.peers[newpeer.ToKey()] = newpeer
	PeerChannel <- newpeer
	InfoLogger.Printf("New peer added %s", newpeer.Name)
}

// Convenience method for concurrency safe removal of peers from the map
func (pm *PeerManager) deletePeer(delpeer *PeerInfo) {
	delete(pm.peers, delpeer.ToKey())
	InfoLogger.Printf("Peer removed %s", delpeer.Name)
}

func StartNetwork(serverPort int, baddr *net.TCPAddr) {
	InfoLogger.Printf("PeerManager starting")
	
	// Initialize PeerManager's peer map and own address
	peerManager.peers = make(PeerMap)
	
	// Generate own peer info
	ownInfo.Address = ""
	ownInfo.ServerPort = serverPort
	ownInfo.Name = uuid.NewRandom().String()
	InfoLogger.Printf("Local TCP server info %s", ownInfo.String())
	
	// Start listening on incoming connections on local port
	go StartConnectionListener(serverPort)
	
	// Initiate bootstrap to given address
	if baddr != nil {
		go StartBootstrap(baddr)
	}
}

var BootstrapRequestChannel = make(chan PeerInfo, 10)
func StartBootstrap(addr *net.TCPAddr) {
	
	// Create temporary peerinfo for bootstrap server
	peerInfo := new(PeerInfo)
	peerInfo.Address = addr.IP.String()
	peerInfo.ServerPort = addr.Port
	peerInfo.Name = ""
	peerInfo.State = StateBootstrapStart
	
	BootstrapRequestChannel<-*peerInfo
	
	go BootstrapProcessor(BootstrapRequestChannel)
}

func BootstrapProcessor(bsReqs <-chan PeerInfo) {
	for {
		InfoLogger.Printf("Waiting for bootstrap")
		newPeer := <-bsReqs
		
		// Don't bootstrap with self or one that already is bootstrapping/bootstrapped
		if newPeer.State != StateBootstrapStart {
			InfoLogger.Printf("Bootstrap state of %s was %s", newPeer.Name, newPeer.State)
			continue
		}
		newPeer.State = StateBootstrapStart
		
		// Try to establish client connection to given server
		addr, err := net.ResolveTCPAddr("tcp", newPeer.ToKey())
		conn, err := net.DialTCP("tcp", nil, addr)
		
		if err != nil {
			InfoLogger.Printf("Unable to initiate bootstrap connection, reason %s", err.Error())
			continue
		}
		
		InfoLogger.Printf("Initiating bootstrap with %s", addr.String())
		
		err = SendBootstrapRequest(conn, &ownInfo)
		if err == nil {
			InfoLogger.Printf("Sent bootstrap response to %s (%s)", newPeer.Name, newPeer.ToKey())
		}
		
		// Add peer to manager
		peerManager.addPeer(&newPeer)
		go ClientConnection(conn)
	}
}

func GetClientConnection(recv *PeerInfo) (*net.TCPConn, error){
	// Try to establish client connection to given server
	addr, err := net.ResolveTCPAddr("tcp", recv.ToKey())
	if err != nil {
		return nil, err
	}
	
	conn, err := net.DialTCP("tcp", nil, addr)
	if err != nil {
		return nil, err
	}
	
	go ClientConnection(conn)
	
	return conn, nil
}

func StartConnectionListener(listenPort int) {
	// Generate local TCP address
	laddr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf(":%d", listenPort))
	if err != nil {
		InfoLogger.Printf("Unable to resolve local address")
		return
	}
	
	// Start TCP listener
	listener, err := net.ListenTCP("tcp", laddr)
	if err != nil {
		InfoLogger.Printf("Unable to start TCP listener on %s", laddr.String())
		return
	}
	
	InfoLogger.Printf("TCP Connection listener started on %s", laddr.String())
	
	for {
		// Accept connections
		newConn, _ := listener.AcceptTCP()
		
		go ServerConnection(newConn)
		
	}
}

func HostPortFromConn(conn *net.TCPConn) string {
	return fmt.Sprintf("%s:%d", conn.RemoteAddr().(*net.TCPAddr).IP, conn.RemoteAddr().(*net.TCPAddr).Port)
}

func ServerConnection(conn *net.TCPConn) {
	// Check if the connected peer is already known
	peerInfo, exists := peerManager.peers[HostPortFromConn(conn)]
	
	// Create new peer object if the client doesn't have one yet
	if !exists {
		InfoLogger.Printf("New peer %s", HostPortFromConn(conn))
		peerInfo = new(PeerInfo)
		peerInfo.Address = conn.RemoteAddr().(*net.TCPAddr).IP.String()
		peerInfo.ServerPort = conn.RemoteAddr().(*net.TCPAddr).Port
		peerInfo.Name = ""
	}
	
	// Read loop
	for {
		// Decode message from stream
		msg, err := DecodeMessage(conn)
		if err != nil && msg == nil {
			InfoLogger.Printf("TCP server connection closing, received %s", err.Error())
			break
		}
		InfoLogger.Printf("Server received %d from %s", msg.MsgType, peerInfo.ToKey())
		
		msgInfo := MessageInfo{peerInfo, msg}
		switch msg.MsgType {
		case MsgBootstrapRequest:
			InfoLogger.Printf("Received bootstrap req")
			BootstrapRequestHandler(conn, &msgInfo)
		case MsgChunkStoreRequest:
			InfoLogger.Printf("Received chunk store req")
			ChunkStoreHandler(conn, &msgInfo)
		case MsgChunkRetrieveRequest:
			InfoLogger.Printf("Received chunk retrieve req")
			ChunkRetrieveReqHandler(conn, &msgInfo)
		}
	}
	
	// Read loop finished, close connection
	conn.Close()
}

func ClientConnection(conn *net.TCPConn) {
	// Create temporary peer object until we receive a message with identification
	InfoLogger.Printf("Peer not in peerlist")
	peerInfo := new(PeerInfo)
	peerInfo.Address = conn.RemoteAddr().(*net.TCPAddr).IP.String()
	peerInfo.ServerPort = conn.RemoteAddr().(*net.TCPAddr).Port
	peerInfo.Name = ""
	
	stop := false
	// Read loop
	for !stop {
		// Decode message from stream
		msg, err := DecodeMessage(conn)
		
		if err != nil && msg == nil {
			InfoLogger.Printf("TCP client connection closing, received %s", err.Error())
			break
		}
		
		InfoLogger.Printf("Client received %d from %s", msg.MsgType, msg.Name)
		
		// Check if the connected peer is already known
		pi, exists := peerManager.peers[HostPortFromConn(conn)]
		if exists {
			peerInfo = pi
		}
		
		msgInfo := MessageInfo{peerInfo, msg}
		switch msg.MsgType {
		case MsgBootstrapResponse:
			InfoLogger.Printf("Received bootstrap resp")
			BootstrapResponseHandler(conn, &msgInfo)
			stop = true
			break
		case MsgChunkRetrieveResponse:
			InfoLogger.Printf("Received chunk retrieve response")
			ChunkRetrieveRespHandler(conn, &msgInfo)
			stop = true
			break
		}
	}

	// Read loop finished, close connection
	conn.Close()
}

func BootstrapRequestHandler(conn *net.TCPConn, msg *MessageInfo) {
	// First update msg.Src port and name fields
	msg.Src.ServerPort = msg.Data.MsgData.(*BootstrapRequest).ServerPort
	msg.Src.Name = msg.Data.Name
	
	peerManager.Lock()
	// Add peer if it doesn't already exist (shouldn't)
	if _, exists := peerManager.peers[msg.Src.ToKey()]; !exists {
		msg.Src.State = StateIdle
		peerManager.addPeer(msg.Src)
	}
	peerManager.Unlock()
	
	err := SendBootstrapResponse(conn, &ownInfo, peerManager)
	if err == nil {
		InfoLogger.Printf("Sent bootstrap response to %s (%s)", msg.Src.Name, msg.Src.ToKey())
	}
}

func BootstrapResponseHandler(conn *net.TCPConn, msg *MessageInfo) {
	// First check if the sender is in our peerlist, discard message if not
	peerManager.Lock()
	defer peerManager.Unlock()
	
	m := msg.Data.MsgData.(*BootstrapResponse)
	
	// Get our reference to peer from peer manager
	p, e := peerManager.peers[msg.Src.ToKey()]
	if !e {
		InfoLogger.Printf("Discarded bootstrap response from unknown peer %s", msg.Src.ToKey())
		return
	}
	
	// Update sender info locally
	if p.Name != msg.Data.Name {
		InfoLogger.Printf("Peer %s name updated from %s to %s", msg.Src.String(), p.Name, msg.Data.Name)
		p.Name = msg.Data.Name
	}
	p.State = StateIdle
	InfoLogger.Printf("Bootstrap finished with %s (%s)",p.Name ,conn.RemoteAddr().String())

	// Start bootstrap with received peers	
	for _, peer := range m.Peers {
		// Discard peers that are already known to us
		_, e := peerManager.peers[peer.ToKey()]
		if peer.Name == ownInfo.Name || e {
			InfoLogger.Printf("Skipped known peer %s (%s)", peer.Name, peer.ToKey())
			continue
		}
		
		// Add new peers to bootstrap request channel to start bootstrapping
		InfoLogger.Printf("Requesting bootstrap with peer %s (%s)", peer.Name, peer.ToKey())
		peer.State = StateBootstrapStart
		BootstrapRequestChannel <- peer
	}
}

func ChunkStoreHandler(conn *net.TCPConn, msg *MessageInfo) {
	ci := msg.Data.MsgData.(*ChunkInfo)
	InfoLogger.Printf("Received chunk %s", ci.ID)
	nc, e := ChunkStorage[ci.ID]
	if e {
		InfoLogger.Printf("Chunk already exists with ID %s", ci.ID.String())
		return
	}
	nc = ci
	ChunkStorage[ci.ID] = nc
}

func ChunkRetrieveReqHandler(conn *net.TCPConn, msg *MessageInfo) {
	cr := msg.Data.MsgData.(*ChunkRetrieveRequest)
	req, e := ChunkStorage[cr.ID]
	if !e {
		req = &ChunkInfo{Index: -1}
	}
	InfoLogger.Printf("Sending chunk retrieve response to %s", msg.Src.Name)
	SendChunkRetrieveResponse(conn, msg.Src, req)
}


func ChunkRetrieveRespHandler(conn *net.TCPConn, msg *MessageInfo) {
	cr := msg.Data.MsgData.(*ChunkRetrieveResponse)
	ChunkRetrievalChannel <- &ChunkInfo{ ID: cr.ID,
										Index: cr.Index,
										Length: cr.Length,
										ChunkData: cr.ChunkData }
}