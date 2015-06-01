package main

import (
	"sync"
	"bytes"
	"encoding/gob"
	"crypto/sha256"
)

// Filelist type represents a map of filename -> fileinfo descriptor
type FileDescriptorList struct {
	sync.RWMutex
	Files 		map[string]FileInfo
}

type FileChunkInfo struct {
	Index	int
	ID		ChunkID
}

// Fileinfo type represents a file descriptor containing it's name, and the encrypted array of chunk IDs
// as byte array
type FileInfo struct {
	FileName	string
	CheckHash	[32]byte
	RawChunks	[]byte
}

type ChunkID 	[16]byte

// Chunkinfo represents a struct containing the chunk ID as well as the encrypted chunk data
type ChunkInfo struct {
	ID			ChunkID
	Index		int
	ChunkData	[]byte
	Length		int
}

// 
type ChunkChannel struct {
	Key			[]byte
	FileName	string
	Chunk		ChunkInfo
}

var (
	FileList				FileDescriptorList
	PeerChannel				= make(chan *PeerInfo, 10)
	ChunkRetrievalChannel 	= make(chan *ChunkInfo, 10)
	ChunkStorage			= make(map[ChunkID]*ChunkInfo)
)

func (cid *ChunkID) String() string {
	return string(append(cid[:]))
}

func StartDistributor(inc <- chan *ChunkChannel) {
	FileList.Files = make(map[string]FileInfo)
	InfoLogger.Printf("ChunkManager starting...")
	for {
		// Get chunk from channel
		InfoLogger.Printf("Waiting for chunks...")
		nc := <-inc
		// Get next peer from channel
		InfoLogger.Printf("Waiting for peers...")
		np := <- PeerChannel

		InfoLogger.Printf("Got peer with state %d", np.State)
		for {
			// Check that peer from channel has finished bootstrap
			if np.State != StateIdle {
				// Put still bootstrapping peers back to channel
				PeerChannel <- np
			} else {
				// Try to establish client connection to given server
				break
			}
			// Get new peer since the last one wasn't ready
			np = <-PeerChannel
		}
		
		// Add chunk to filelist
		FileList.AddChunkID(nc.FileName, nc.Chunk.ID, nc.Chunk.Index, nc.Key)
		
		err := SendChunkStoreRequest(np, nc.Chunk)
		if err == nil {
			InfoLogger.Printf("Sent chunk store request message with chunk id %s", nc.Chunk.ID)
		}
		
		// Put peer back to channel for next chunk
		PeerChannel <- np
	}
}

func StartChunkRetriever(fn string, password []byte, done chan<- *ChunkInfo) {
	// Get the raw chunk data for the requested file
	FileList.RLock()
	chunks := FileList.Files[fn].RawChunks
	FileList.RUnlock()
	
	// Generate decryption key
	key := sha256.Sum256([]byte(password))
	
	// Decrypt raw chunk bytes
	raw := Decrypt(key[:], chunks)
	
	// Decode chunk bytes into array
	var ids []FileChunkInfo
	buf := bytes.NewBuffer([]byte(raw))
	dec := gob.NewDecoder(buf)
	err := dec.Decode(&ids)
	if err != nil {
		InfoLogger.Printf("Error decoding ChunkID list, %s", err)
		return
	}
	
	InfoLogger.Printf("Chunks %s", ids)
	
	// Define a list to check if a chunk has already been sent to downloader
	b := make([]bool, len(ids))
	// Amount of chunks to get
	count := len(ids)
	
	// Request each chunk from each peer
	peerManager.RLock()
	for _, c := range ids {
		for _, p := range peerManager.peers {
			SendChunkRetrieveRequest(p, c.ID)
		}
	}
	peerManager.RUnlock()
	
	// Wait for responses
	for {
		c := <- ChunkRetrievalChannel
		// check if this is a valid chunk
		if c.Index < 0 {
			continue
		}
		
		// check if this chunk was already received
		if b[c.Index] {
			continue
		}
		done <- c
		// Set chunk as received
		b[c.Index] = true
		// Decrement count and check if all are received
		count = count - 1
		if count == 0 {
			close(done)
			return
		}
	}
}

// AddChunkID adds given chunk id to the list of chunk id's of given filename. If filename doesn't exist, it is created.
func (fl *FileDescriptorList) AddChunkID(fn string, id ChunkID, index int, key []byte) {
	fl.Lock()
	defer fl.Unlock()
	
	var infos []FileChunkInfo
	f, exists := fl.Files[fn]
	
	// Initialize fileinfo if it didn't exist
	if !exists {
		ch := sha256.Sum256(key)
		InfoLogger.Printf("New fileinfo for %s, check hash %x", fn, ch)
		f = FileInfo{FileName: fn, CheckHash: ch}
	} else {
		InfoLogger.Printf("Adding chunk for %s", fn)
		// Fileinfo existed, so decrypt the chunk ids
		raw := Decrypt(key, f.RawChunks)
		// And decode
		buf := bytes.NewReader([]byte(raw))
		dec := gob.NewDecoder(buf)
		dec.Decode(&infos)
	}
	
	// Append new id to end of existing ones
	infos = append(infos, FileChunkInfo{ ID: id, Index: index } )
	
	// Encode into binary
	buf := bytes.NewBuffer(nil)
	enc := gob.NewEncoder(buf)
	enc.Encode(infos)

	// Encrypt encoded buffer and store in descriptor
	f.RawChunks = []byte(Encrypt(key, buf.Bytes()))
	
	fl.Files[fn] = f
}

// GetChunksForFile gets an array containing all the chunk id's for a given file
func (fl *FileDescriptorList) GetChunksForFile(fn string, key []byte) []FileChunkInfo {
	fl.RLock()
	defer fl.Unlock()
	// Initialize empty return array
	ret := make([]FileChunkInfo, 0)
	
	// Get fileinfo for fn
	f, exists := fl.Files[fn]
	if !exists {
		// fn doesn't exist, so return the empty array
		InfoLogger.Printf("File %s doesn't exist", fn)
		return ret
	}
	
	// Fileinfo existed, so decrypt the chunk ids
	raw := Decrypt(key, f.RawChunks)
	// And decode
	buf := bytes.NewBuffer(raw)
	dec := gob.NewDecoder(buf)
	dec.Decode(&ret)
	
	return ret
}

// CheckPassword checks whether given password is correct for given file
func CheckPassword(key []byte, fn string) bool {
	FileList.RLock()
	defer FileList.RUnlock()
	f := FileList.Files[fn]
	
	ch := sha256.Sum256(key)
	ch = sha256.Sum256(ch[:])
	return ch == f.CheckHash
}
