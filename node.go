package main

import (
	"os"
	"net/http"
	"net"
	"html/template"
	"log"
	"mime/multipart"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"io"
	"flag"
	"fmt"
)

type Page struct {
	Error 	[]byte
	Body 	[]byte
	Peers 	[]PeerInfo
	Name	string
}

var (
	InfoLogger	*log.Logger
	ChunkQueue	chan *ChunkChannel
)

func Encrypt(key, plaintext []byte, index int64) string {
    block, err := aes.NewCipher(key)
    if err != nil {
        panic(err)
    }
    
    // Prepend data with index and size
	indBytes := make([]byte, binary.MaxVarintLen32 + binary.MaxVarintLen32)
	count := binary.PutVarint(indBytes, index)
	l := int64(len(plaintext))
	binary.PutVarint(indBytes[count:], l)
	plaintext = append(indBytes, plaintext...)
	
	// Initialize byte array to hold initialization vector (size is AES block size)
	// and the cipher text
    ciphertext := make([]byte, aes.BlockSize + len(plaintext))
    iv := ciphertext[:aes.BlockSize]
    // Generate unique IV from crypto random generator
    if _, err := io.ReadFull(rand.Reader, iv); err != nil {
        panic(err)
    }
    
    // Get the feedback mode encrypter using our AES block and IV
    stream := cipher.NewCFBEncrypter(block, iv)
    // Encrypt plaintext, leave the IV in the beginning
    stream.XORKeyStream(ciphertext[aes.BlockSize:], plaintext)
    
    // Finally encode in base64 and return the resulting ciphertext
    return base64.StdEncoding.EncodeToString(ciphertext)
}

func Decrypt(key, encoded_ciphertext []byte) (string, int64) {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	
	// Decode as base64
	ciphertext := make([]byte, base64.StdEncoding.EncodedLen(len(encoded_ciphertext)))
	_, err = base64.StdEncoding.Decode(ciphertext, encoded_ciphertext[:])
	
	if err != nil {
		panic(err)
	}
	
	// The IV should be included in the beginning
	if len(ciphertext) < aes.BlockSize {
		panic("Ciphertext too short")
	}
	
	// Separate IV from ciphertext
	iv := ciphertext[:aes.BlockSize]
	ciphertext = ciphertext[aes.BlockSize:]
	
	stream := cipher.NewCFBDecrypter(block, iv)
	stream.XORKeyStream(ciphertext, ciphertext)
	
    // Decode index from beginning of plaintext
	index, count := binary.Varint(ciphertext)
	length, c := binary.Varint(ciphertext[count:])
	ciphertext = ciphertext[count+c:length]
	
	// Finally return the resulting plaintext
	return string(ciphertext), index
}

func FileUploader(fileHandle multipart.File, fileHeader multipart.FileHeader, password string) {
	defer fileHandle.Close()		
	
	// Calculate sha256 checksum for password
	hash := sha256.Sum256([]byte(password))
	index := int64(0)
	
	for {
		// Read data into 1kB chunks
		data := make([]byte, 1024)
		byteCount, err := fileHandle.Read(data)
		
		// Break if no data was read
		if byteCount == 0 {
			break
		}
		
		// Encrypt the data
		ct := Encrypt(hash[:], data, index)
		InfoLogger.Printf("Encrypted #%d with %d bytes: %s", index, len(ct), ct)
		
		// Generate ID array from first 16 bytes of cipher text, and create ChunkInfo struct
		var id [16]byte
		for i, c := range []byte(ct[:16]) {
			id[i] = c
		}
		ci := ChunkInfo{ ID: id,
						 ChunkData: []byte(ct) }
		// Put into chunk queue
		cc := ChunkChannel{ Chunk: ci,
							FileName: fileHeader.Filename,
							Key: hash[:] }
		
		InfoLogger.Printf("Putting chunk to ChunkManager..")
		ChunkQueue <- &cc
		
		//Decrypt the data
		//pt, index := Decrypt(hash[:], []byte(ct))
		//InfoLogger.Printf("Decrypted #%d: %s", index, pt)
		
		// check if eof reached
		if err != nil {
			break
		}
		
		index = index + 1
	}
}

const indexTemplate = `
	<html>
	<head>
	<title>Distributed file storage</title>
	</head>
	<body>
	<h1>{{.Name}}</h1><br />
	<b>{{printf "%s" .Error}}</b><br />
	<form method="POST" action="/" enctype="multipart/form-data">
	File: <input type="file" name="file" /><br />
	Password: <input type="text" name="password" /><br />
	<input type="submit" value="Submit" /><br />
	<table>
	{{range $peer := .Peers}}
	<tr><td>{{$peer.Name}}</td><td>{{$peer.Address}}:{{$peer.ServerPort}}</td></tr>
	{{end}}
	</table>
	{{printf "%s" .Body}}
	</form>
	
	</body>
	</html>`
func IndexHandler(writer http.ResponseWriter, req *http.Request) {
	InfoLogger.Printf("Request for index page")
	
	// Initialize struct for page template
	page := &Page{}
	
	// Get all peers from peerlist and add them to page template
	peerManager.RLock()
	p := make([]PeerInfo, len(peerManager.peers))
	i := 0
	for _, peer := range peerManager.peers {
		p[i] = *peer
		i = i + 1
	}
	peerManager.RUnlock()
	page.Peers = p
	
	// Add own name
	page.Name = ownInfo.Name
	
	// First handle form data if any
	file, fileHeader, err := req.FormFile("file")
	password := req.FormValue("password")
	if err != nil || len(password) == 0 {
		page.Error = []byte("No password or file given")
	} else {
		FileUploader(file, *fileHeader, password)
		InfoLogger.Printf("Uploaded %s with %s as password", fileHeader.Filename, password)
	}
	
	page.Body = []byte("Hello world")
    t, _ := template.New("index").Parse(indexTemplate)
    t.Execute(writer, page)
}

func main() {
	// Init logging
	InfoLogger = log.New(os.Stdout, "[MAIN][INFO] ", log.LstdFlags)
	
	// Get command line parameters
	var webServerPort int
	var serverPort int
	var bootstrap string
	
	flag.IntVar(&webServerPort, "http", 8080, "Web server HTTP port")
	flag.IntVar(&serverPort, "serverport", 10001, "TCP port used to listen for peer connections with other nodes")
	flag.StringVar(&bootstrap, "bootstrap", "", "Optional address of a node to use bootstrapping into network. Format is ip:port")
	flag.Parse()
	
	// Start chunk manager
	ChunkQueue = make(chan *ChunkChannel, 10)
	go StartChunkManager(ChunkQueue)
	
	// Start peermanager
	if bootstrap != "" {
		bootstrapAddr, err := net.ResolveTCPAddr("tcp4", bootstrap)
		if err != nil {
			InfoLogger.Printf("No bootstrap address given")
		}
		go StartNetwork(serverPort, bootstrapAddr)
	} else {
		go StartNetwork(serverPort, nil)
	}
	
	http.HandleFunc("/", IndexHandler)
	// Try starting webserver, incrementing port every time it fails
	for {
		InfoLogger.Printf("Starting HTTP interface :%d", webServerPort)
		err := http.ListenAndServe(fmt.Sprintf(":%d", webServerPort), nil)
		if err != nil {
			InfoLogger.Printf("Port already in use :%d", webServerPort)
			webServerPort = webServerPort + 1
		}
	}
}
