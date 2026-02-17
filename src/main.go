package main

import (
	"fmt"
	"io"
	"net"
	"os"
)
func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run tcp.go <port>")
		os.Exit(1)
	}

	port := fmt.Sprintf(":%s", os.Args[1])

	listener, err := net.Listen("tcp", port)

	if err != nil {
		fmt.Println("Failed ot create listener, err:", err)
		os.Exit(1)
	}
	defer listener.Close()
	fmt.Printf("listening on %s\n", listener.Addr())

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("failed to accept conneciton, err",  err)
			continue
		}

		go handleConnection(conn)
	}
}

func handleConnection(con net.Conn) {
	defer con.Close()

	buf := make([]byte, 4096)
	for {
		n, err := con.Read(buf)
		if err != nil {
			if err == io.EOF {
				break
			}
			return 
		}
		if string(buf[:n]) == "q" {
			fmt.Print("Goodbye")
			return
		}	
		con.Write(buf[:n])
	}
}