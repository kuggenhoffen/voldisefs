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
	// Channel for incoming messages, PeerConnectionHandlers generate, MessageHandler consumes
	incomingMsgChannel 		= make(chan MessageInfo, 20)
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

var BootstrapRequestChannel = make(chan *PeerInfo, 1)
func StartBootstrap(addr *net.TCPAddr) {
	
	// Create temporary peerinfo for bootstrap server
	peerInfo := new(PeerInfo)
	peerInfo.Address = addr.IP.String()
	peerInfo.ServerPort = addr.Port
	peerInfo.Name = ""
	peerInfo.StateBootstrap = StateBootstrapNone
	
	BootstrapRequestChannel<-peerInfo
	
	go BootstrapProcessor(BootstrapRequestChannel, PeerChannel)
}

func BootstrapProcessor(bsReqs <-chan *PeerInfo, pi chan<- *PeerInfo) {
	for {
		InfoLogger.Printf("Waiting for bootstrap")
		newPeer := <-bsReqs
		
		// Don't bootstrap with self or one that already is bootstrapping/bootstrapped
		if newPeer.StateBootstrap != StateBootstrapNone {
			continue
		}
		newPeer.StateBootstrap = StateBootstrapWait
		
		// Try to establish client connection to given server
		addr, err := net.ResolveTCPAddr("tcp", newPeer.ToKey())
		conn, err := net.DialTCP("tcp", nil, addr)
		
		
		if err != nil {
			InfoLogger.Printf("Unable to initiate bootstrap connection, reason %s", err.Error())
			continue
		}
		
		InfoLogger.Printf("Initiating bootstrap with %s", addr.String())
		
		peerManager.Lock()
		peerManager.addPeer(newPeer)
		peerManager.Unlock()
		
		err = SendBootstrapRequest(conn, &ownInfo)
		if err == nil {
			InfoLogger.Printf("Sent bootstrap response to %s (%s)", newPeer.Name, newPeer.ToKey())
		}
		
		go ClientConnection(conn)
	}
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
		InfoLogger.Printf("Peer not in peerlist")
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
		}
	}
	
	// Read loop finished, close connection
	conn.Close()
}

func ClientConnection(conn *net.TCPConn) {
	// Check if the connected peer is already known
	peerInfo, exists := peerManager.peers[HostPortFromConn(conn)]
	
	// Create new peer object if one wasn't supplied
	if !exists {
		InfoLogger.Printf("Peer not in peerlist")
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
			InfoLogger.Printf("TCP client connection closing, received %s", err.Error())
			break
		}
		
		InfoLogger.Printf("Client received %d from %s", msg.MsgType, peerInfo.ToKey())
		
		msgInfo := MessageInfo{peerInfo, msg}
		switch msg.MsgType {
		case MsgBootstrapResponse:
			InfoLogger.Printf("Received bootstrap resp")
			BootstrapResponseHandler(conn, &msgInfo)
			break
		}
	}

	// Read loop finished, close connection
	conn.Close()
}

func BootstrapRequestHandler(conn *net.TCPConn, msg *MessageInfo) {
	// First update msg.Src port and name fields
	msg.Src.ServerPort = msg.Data.MsgData.(*BootstrapRequest).ServerPort
	msg.Src.Name = msg.Data.MsgData.(*BootstrapRequest).Name
	peerManager.Lock()
	if _, exists := peerManager.peers[msg.Src.ToKey()]; !exists {
		peerManager.addPeer(msg.Src)
	}
	peerManager.Unlock()
	peerManager.RLock()
	err := SendBootstrapResponse(conn, &ownInfo, peerManager)
	peerManager.RUnlock()
	if err == nil {
		InfoLogger.Printf("Sent bootstrap response to %s (%s)", msg.Src.Name, msg.Src.ToKey())
	}
}

func BootstrapResponseHandler(conn *net.TCPConn, msg *MessageInfo) {
	// First check if the sender is in our peerlist, discard message if not
	peerManager.Lock()
	defer peerManager.Unlock()
	if _, exists := peerManager.peers[msg.Src.ToKey()]; !exists {
		InfoLogger.Printf("Discarded bootstrap response from unknown peer %s", msg.Src.ToKey())
		return
	}
	
	// Check if sender is in our peerlist and update it's name if so
	if p, exists := peerManager.peers[msg.Src.ToKey()]; exists {
		if p.Name != msg.Data.MsgData.(*BootstrapResponse).Name {
			p.Name = msg.Data.MsgData.(*BootstrapResponse).Name
			InfoLogger.Printf("Existing peer %s name updated %s", conn.RemoteAddr().String(), p.Name)
		}
	}
	
	for _, peer := range msg.Data.MsgData.(*BootstrapResponse).Peers {
		// Discard own peerinfo
		if peer.Name == ownInfo.Name {
			continue
		}
		
		// Update peer name if it doesn't match (happens after bootstrap)
		if p, exists := peerManager.peers[peer.ToKey()]; exists {
			if p.Name != peer.Name {
				peer.Name = p.Name
			}
		} else {
			// Otherwise put peer into request channel for requesting bootstrap
			BootstrapRequestChannel <- &peer
		}
	}
}