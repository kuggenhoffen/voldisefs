#Distributed systems project
#Volatile, distributed and secure file storage

Voldisefs is a distributed peer-to-peer file storage network. The purpose of the project is to be able to store files
remotely, so that no third party shall be able to access the file contents without knowledge of the password used to store it 
securely, the files are encrypted with a password using AES256 encryption with the aim that no third party shall be able to access them even if they managed to get ahold of them. Additionally each file and it's metadata is encrypted with it's own password
in a volatile, transitory manner, where the peer loses all it's store local contents when it shutsdown.

## Core functionality
The following functionality is implemented and necessary for the operation of the network:
- The peers communicate with each other using a TCP server for receiving requests from other peers and sending responses to other peers; while a TCP client is used for sending requests and receiving responses
- A peer is able to bootstrap to an existing network by initiating a bootstrap request to a known peer in the network. The bootstrap process involves requesting the given peer for it's known peers, recursively requesting known peers of every single peer in the network resulting in a fully replicated network state across all peers.
- A peer implements an HTTP server for uploading and downloading of files as well as inspecting the network state
- A peer is able to split an uploaded file into chunks, and encrypting each chunk with a given password
- A peer is able to distribute (sequentially) encrypted filechunks to it's known peers in the network
- A peer is able to store file metadata, filename and encrypted chunk metadata) locally
- A peer is able to request chunks for given file from the known peers
- A peer is able to combine the received chunks into the original file

## Principles
### Architecture
The system implements a distributed decentralized peer-to-peer architecture. The system doesn't implement anykind of overlay network topology management other than full network state replication in each node resulting in a flat peer-to-peer network.
### Processes
The nodes utilize multiprocessing to separate processing of webserver, TCP client and TCP server to avoid blocking of communication between user and the node as well as between nodes.
### Communication
The communication between nodes utilizes JSON encoded messages over TCP connection. This result in discrete and asynchronous communication so that multiple clients can be served at the same time.
### Naming
Each node generates a universally unique identifier as it's name. This name is used to map between a nodes server connection, with static port number, and client connections, which may use any port. This results in an unstructured flat naming namespace where the name is merely used to map the node name to it's TCP connections.
### Synchronisation
The system doesn't implement any method of synchronisation between nodes other than the bootstrapping process. After that the node's don't require any synchronisation.
### Consistency and replication
No specific consistency or replication procedures are implemented.
### Fault tolerance
Some fault tolerance is achieved at the process level by the multiprocessing architecture. Exceptions and errors in communication with a node don't affect the operability of the node or the communication with other nodes. There is no fault tolerance in the chunk storage implemented in that once a server disconnects, all the chunks stored in that server are lost.
### Security
No security is implemented for the node communications. The stored data itself is encrypted by the requesting node.

## Reflections
The main problem was the lack of time because of very late start. This was not a problem of the project itself, rather a personal failure to start work earlier. Earlier start would have resulted in more detailed design and in turn easier and more refined implementation.
Implementation using the Go language presented some hurdles, mainly because of the lack of experience using it in more complicated applications. Understanding the concurrent programming and using the provided tools correctly required some studying. Also the lack of understanding of some of the basic idioms of Go resulted in it's own challenges.
