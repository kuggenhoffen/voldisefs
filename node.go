package main

import (
	"os"
	"net/http"
	"html/template"
	"log"
	"mime/multipart"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"
)

type Page struct {
	Error []byte
	Body []byte
}

var (
	InfoLogger	*log.Logger
)

func Encrypt(key, plaintext []byte) string {
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
    return base64.StdEncoding.EncodeToString(ciphertext)
}

func Decrypt(key, encoded_ciphertext []byte) string {
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
	
	// Finally return the resulting plaintext
	return string(ciphertext)
}

func FileUploader(fileHandle multipart.File, fileHeader multipart.FileHeader, password string) {
	defer fileHandle.Close()
	for {
		// Read 1kB of data to buffer at a time
		data := make([]byte, 1024)
		byteCount, err := fileHandle.Read(data)
		
		// Break if no data was read
		if byteCount == 0 {
			break
		}
		
		InfoLogger.Printf("Read %d bytes: %s", byteCount, string(data))
		
		// Calculate sha256 checksum for password
		hash := sha256.Sum256([]byte(password))
		// Encrypt the data
		ct := Encrypt(hash[:], data)
		InfoLogger.Printf("Encrypted %d bytes: %s", len(ct), ct)
		
		//Decrypt the data
		pt := Decrypt(hash[:], []byte(ct))
		InfoLogger.Printf("Decrypted: %s", pt)
		
		// check if eof reached
		if err != nil {
			break
		}
	}
}

func IndexHandler(writer http.ResponseWriter, req *http.Request) {
	InfoLogger.Printf("Request for index page")
	
	// Initialize struct for page template
	page := &Page{}
	
	// First handle form data if any
	file, fileHeader, err := req.FormFile("file")
	password := req.FormValue("password")
	if err != nil || len(password) == 0 {
		page.Error = []byte("No password or file given")
		return
	}
	
	FileUploader(file, *fileHeader, password)
	
	InfoLogger.Printf("Uploaded %s with %s as password", fileHeader.Filename, password)
	
	page.Body = []byte("Hello world")
    t, _ := template.ParseFiles("index.html")
    t.Execute(writer, page)
}

func ConnectionManager() {
	
}

func main() {
	// Init logging
	InfoLogger = log.New(os.Stdout, "INFO: ", log.LstdFlags)
	
	
	
	http.HandleFunc("/", IndexHandler)
	http.ListenAndServe(":8000", nil)
}
