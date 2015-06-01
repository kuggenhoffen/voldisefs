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
	"io"
	"flag"
	"fmt"
	"strings"
)

type Page struct {
	Error 	string
	Info	string
	Peers 	[]PeerInfo
	Name	string
	Chunks	[]string
	Files	[]string
}

var (
	InfoLogger	*log.Logger
	ChunkQueue	chan *ChunkChannel
)

func Encrypt(key, plaintext []byte) []byte {
    block, err := aes.NewCipher(key)
    if err != nil {
        panic(err)
    }
    
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
    return []byte(base64.StdEncoding.EncodeToString(ciphertext))
}

func Decrypt(key, encoded_ciphertext []byte) []byte {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	
	// Decode as base64
	ciphertext := make([]byte, base64.StdEncoding.DecodedLen(len(encoded_ciphertext)))
	_, err = base64.StdEncoding.Decode(ciphertext, encoded_ciphertext)
	
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
	
	// Finally return the resulting plaintext
	return ciphertext
}

func KeyFromPassword(p string) []byte {
	hash := sha256.Sum256([]byte(p))
	return hash[:]
}

func FileUploader(fileHandle multipart.File, fileHeader multipart.FileHeader, password string) {
	defer fileHandle.Close()		
	
	// Calculate sha256 checksum for password
	hash := KeyFromPassword(password)
	index := 0
	
	for {
		// Read data into 1kB chunks
		data := make([]byte, 1024)
		byteCount, err := fileHandle.Read(data)
		
		// Break if no data was read
		if byteCount == 0 {
			break
		}
		
		// Encrypt the data
		ct := Encrypt(hash[:], data)
		InfoLogger.Printf("Encrypted #%d with %d bytes: %s", index, byteCount, ct)
		
		// Generate ID array from first 16 bytes of cipher text, and create ChunkInfo struct
		var id [16]byte
		for i, c := range []byte(ct[:16]) {
			id[i] = c
		}
		ci := ChunkInfo{ ID: id,
						 Index: index,
						 Length: byteCount,
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
	<b><font color="red">{{printf "%s" .Error}}</font></b><br />
	<b><font color="green">{{printf "%s" .Info}}</font></b><br />
	<form method="POST" action="/" enctype="multipart/form-data">
	File: <input type="file" name="file" /><br />
	Password: <input type="text" name="password" /><br />
	<input type="submit" value="Submit" /><br />
	</form>
	
	<table border="1px; solid; #000000">
	<tr><td colspan="3">Connected peers</td></tr>
	<tr><td>Name</td><td>Address</td><td>State</td></tr>
	{{range $peer := .Peers}}
	<tr><td>{{$peer.Name}}</td><td>{{$peer.Address}}:{{$peer.ServerPort}}</td><td>{{$peer.State}}</td></tr>
	{{end}}
	</table>
	
	<hr />
	<table border="1px; solid; #000000">
	<tr><td colspan="3">Stored files</td></tr>
	<tr><td>ID</td><td>Password</td><td>Download</td>
	{{range $file := .Files}}
	<form method="POST" action="/download/"><tr>
	<td>{{$file}}<input type="hidden" name="filename" value="{{$file}}" /></td>
	<td><input type="password" name="password" /></td>
	<td><input type="submit" value="Download" /></td>
	</tr></form>
	{{end}}
	</table>
	
	<hr />
	<table border="1px; solid; #000000">
	<tr><td>Stored chunks</td></tr>
	<tr><td>ID</td>
	{{range $chunk := .Chunks}}
	<tr><td>{{$chunk}}</td></tr>
	{{end}}
	</table>
	</form>
	
	</body>
	</html>`
func IndexRenderer(writer *http.ResponseWriter, req *http.Request, e string, info string) {
	// Initialize struct for page template
	page := &Page{}
		
	// Populate page structure values
	page.Name = ownInfo.Name
	page.Info = info
	
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
	
	// Get local chunks
	page.Chunks = make([]string, len(ChunkStorage))
	i = 0
	for _, c := range ChunkStorage {
		page.Chunks[i] = (*c).ID.String()
		i = i + 1
	}
	
	// Get files
	FileList.RLock()
	page.Files = make([]string, len(FileList.Files))
	i = 0
	for _, f := range FileList.Files {
		page.Files[i] = f.FileName
		i = i + 1
	}
	FileList.RUnlock()

	page.Error = e
	
    t, err := template.New("index").Parse(indexTemplate)
    if err != nil {
    	panic(err)
    }
    t.Execute(*writer, page)
}

func IndexHandler(writer http.ResponseWriter, req *http.Request) {
	e := ""
	i := ""
	InfoLogger.Printf("Index requested")
	// Handle form data
	if strings.ToUpper(req.Method) == "POST" {
		// Validate form inputs
		file, fileHeader, err := req.FormFile("file")
		if err != nil {
			e = fmt.Sprintf("File error %s", err.Error())
		}
		password := req.FormValue("password")
		if len(password) == 0 {
			e = "No password was given"
		} else {
			// Valid form inputs, call file uploader
			FileUploader(file, *fileHeader, password)
			i = "File uploaded successfully"
			InfoLogger.Printf("Uploaded %s with %s as password", fileHeader.Filename, password)
		}
	}
	
	IndexRenderer(&writer, req, e, i)
}

func DownloadHandler(w http.ResponseWriter, req *http.Request) {
	InfoLogger.Printf("Download requested")
	// Validate request method
	if strings.ToUpper(req.Method) != "POST" {
		IndexRenderer(&w, req, "Download request had invalid HTTP method field", "")
		return
	}
	
	// Validate form data
	password := []byte(req.FormValue("password"))
	filename := req.FormValue("filename")
	if len(password) == 0 || len(filename) == 0 {
		IndexRenderer(&w, req, "Download request had invalid password or filename", "")
		return
	}
	
	// Validate password
	if !CheckPassword(password, filename) {
		IndexRenderer(&w, req, "Wrong password", "")
		return
	}

	ready := make(chan *ChunkInfo, 10)	
	go StartChunkRetriever(filename, password, ready)
	
	var q 	[]*ChunkInfo
	// Loop over channel range
	for nc := range ready {
		InfoLogger.Printf("Received %d chunk size %d..", nc.Index, len(nc.ChunkData))
		q = append(q, nc)
	}
	
	// Order the chunks
	b := make([]*ChunkInfo, len(q))
	for _, n := range q {
		b[n.Index] = n
	}
	
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.Header().Set("Content-type", "application/octet-stream")
	for _, n := range b {
		d := Decrypt(KeyFromPassword(string(password)), n.ChunkData)
		w.Write(d[:n.Length])
	}
}

func main() {
	// Init logging
	InfoLogger = log.New(os.Stdout, "[INFO] ", log.LstdFlags)

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
	go StartDistributor(ChunkQueue)
	
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
	http.HandleFunc("/download/", DownloadHandler)
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
