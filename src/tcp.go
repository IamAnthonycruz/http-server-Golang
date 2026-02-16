package tcp

import (
	"log"
	"net"
)

func main() {
	l, err := net.Listen("tcp", ":1234")
	if err != nil {
		log.Fatal(err)
	}
	defer l.Close()

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Fatal(err)
		}
		go handleConn(conn)

		
	}
}

func handleConn(conn net.Conn) {
	buf := make([]byte, 4096)
	conn.Read(buf)
	n := len(buf)
	conn.Write(buf[:n])
	conn.Close()
}