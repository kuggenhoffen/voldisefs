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
	
	go BootstrapProcessor(BootstrapRequestChannel)
}

func BootstrapProcessor(bsReqs <-chan *PeerInfo) {
	for {
		InfoLogger.Printf("Waiting for bootstrap")
		newPeer := <-bsReqs
		
		if newPeer.StateBootstrap != StateBootstrapNone {
			continue
		}
		
		// Try to establish client connection to given server
		addr, err := net.ResolveTCPAddr("tcp", newPeer.ToKey())
		conn, err := net.DialTCP("tcp", nil, addr)
		
		
		if err != nil {
			InfoLogger.Printf("Unable to initiate bootstrap connection, reason %s", err.Error())
			continue
		}
		
		InfoLogger.Printf("Initiating bootstrap with %s", addr.String())
		
		newPeer.StateBootstrap = StateBootstrapWait
		
		peerManager.Lock()
		peerManager.addPeer(newPeer)
		peerManager.Unlock()
		
		SendBootstrapRequest(conn, &ownInfo)
		
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
		
		go ClientConnection(newConn)
		
	}
}

func HostPortFromConn(conn *net.TCPConn) string {
	return fmt.Sprintf("%s:%d", conn.RemoteAddr().(*net.TCPAddr).IP, conn.RemoteAddr().(*net.TCPAddr).Port)
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
			InfoLogger.Printf("TCP connection closing, received %s", err.Error())
			break
		}
		InfoLogger.Printf("Message %d received from %s", msg.MsgType, peerInfo.ToKey())
		
		msgInfo := MessageInfo{peerInfo, msg}
		switch msg.MsgType {
		case MsgBootstrapRequest:
			InfoLogger.Printf("Received bootstrap req")
			BootstrapRequestHandler(conn, &msgInfo)
		case MsgBootstrapResponse:
			InfoLogger.Printf("Received bootstrap resp")
			BootstrapResponseHandler(conn, &msgInfo)
		}
	}
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
	SendBootstrapResponse(conn, &ownInfo, peerManager)
	peerManager.RUnlock()
}

func BootstrapResponseHandler(conn *net.TCPConn, msg *MessageInfo) {
	// First check if the sender is in our peerlist, discard message if not
	peerManager.Lock()
	defer peerManager.Unlock()
	if _, exists := peerManager.peers[msg.Src.ToKey()]; !exists {
		InfoLogger.Printf("Discarded bootstrap response from unknown peer %s", msg.Src.ToKey())
		return
	}
	
	for _, peer := range msg.Data.MsgData.(*BootstrapResponse).Peers {
		if p, exists := peerManager.peers[peer.ToKey()]; exists {
			// Update peer name if it doesn't match (happens after bootstrap)
			if p.Name != peer.Name {
				peer.Name = p.Name
			}
		} else {
			// Peer isn't in our list yet, check it's not us and add it
			if peer.ToKey() != ownInfo.ToKey() {
				// Put peer into request channel for requesting bootstrap
				BootstrapRequestChannel <- &peer
			}
		}
	}
}