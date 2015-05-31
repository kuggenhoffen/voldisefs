package main

import (
	"sync"
	"bytes"
	"encoding/binary"
)

// Filelist type represents a map of filename -> fileinfo descriptor
type FileDescriptorList struct {
	sync.RWMutex
	Files 		map[string]FileInfo
}

// Fileinfo type represents a file descriptor containing it's name, and the encrypted array of chunk IDs
// as byte array
type FileInfo struct {
	FileName	string
	RawChunks	[]byte
}

type ChunkID 	[16]byte

// Chunkinfo represents a struct containing the chunk ID as well as the encrypted chunk data
type ChunkInfo struct {
	ID			ChunkID
	ChunkData	[]byte
}

// 
type ChunkChannel struct {
	Key			[]byte
	FileName	string
	Chunk		ChunkInfo
}

var (
	FileList		FileDescriptorList
	PeerChannel		= make(chan *PeerInfo, 10)
	ChunkStorage	= make(map[ChunkID]*ChunkInfo)
)

func (cid *ChunkID) String() string {
	return string(append(cid[:]))
}

func StartChunkManager(inc <- chan *ChunkChannel) {
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
		FileList.AddChunkID(nc.FileName, nc.Chunk.ID, nc.Key)
		
		err := SendChunkStoreRequest(np, nc.Chunk)
		if err == nil {
			InfoLogger.Printf("Sent chunk store request message with chunk id %s", nc.Chunk.ID)
		}
		
		// Put peer back to channel for next chunk
		PeerChannel <- np
	}
}

// AddChunkID adds given chunk id to the list of chunk id's of given filename. If filename doesn't exist, it is created.
func (fl *FileDescriptorList) AddChunkID(fn string, id ChunkID, key []byte) {
	fl.Lock()
	defer fl.Unlock()
	
	var ids []ChunkID
	f, exists := fl.Files[fn]
	
	// Initialize fileinfo if it didn't exist
	if !exists {
		InfoLogger.Printf("New fileinfo for %s", fn)
		f = FileInfo{FileName: fn}
	} else {
		InfoLogger.Printf("Updating fileinfo for %s", fn)
		// Fileinfo existed, so decrypt the chunk ids
		raw, _ := Decrypt(key, f.RawChunks)
		// And decode
		buf := bytes.NewReader([]byte(raw))
		binary.Read(buf, binary.BigEndian, ids)
	}
	
	// Append new id to end of existing ones
	ids = append(ids, id)
	// Encode into binary
	buf := bytes.NewBuffer(nil)
	binary.Write(buf, binary.BigEndian, ids)
	// Encrypt encoded buffer and store in descriptor
	f.RawChunks = []byte(Encrypt(key, buf.Bytes(), 0))
	InfoLogger.Printf("Added chunk %s", f.RawChunks)
	
	fl.Files[fn] = f
}

// GetChunksForFile gets an array containing all the chunk id's for a given file
func (fl *FileDescriptorList) GetChunksForFile(fn string, key []byte) []ChunkID {
	fl.RLock()
	defer fl.Unlock()
	// Initialize empty return array
	ret := make([]ChunkID, 0)
	
	// Get fileinfo for fn
	f, exists := fl.Files[fn]
	if !exists {
		// fn doesn't exist, so return the empty array
		InfoLogger.Printf("File %s doesn't exist", fn)
		return ret
	}
	
	// Fileinfo existed, so decrypt the chunk ids
	raw, _ := Decrypt(key, f.RawChunks)
	// And decode
	buf := bytes.NewReader([]byte(raw))
	binary.Read(buf, binary.BigEndian, ret)
	
	return ret
}
