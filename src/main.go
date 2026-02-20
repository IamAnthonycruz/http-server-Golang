package main

import (
	"bufio"
	"fmt"
	"net"
	"strings"
)

func handleConn(conn net.Conn) {
    defer conn.Close()

    reader := bufio.NewReader(conn)

    for {
        // ReadString blocks until it finds '\n' or hits an error.
        // Internally it does exactly what our manual loop above does.
        line, err := reader.ReadString('\n')

        // Note: line may be non-empty even when err != nil.
        // This happens on EOF without a trailing newline — protocol error.
        if len(line) > 0 {
            text := strings.TrimRight(line, "\n")
            if text == "quit" {
                conn.Write([]byte("Bye.\n"))
                return
            }
            conn.Write([]byte("Echo: " + text + "\n"))
        }

        if err != nil {
            if err.Error() != "EOF" {
                fmt.Println("connection error:", err)
            }
            return
        }
    }
}

func main() {
    ln, err := net.Listen("tcp", ":8080")
    if err != nil {
        panic(err)
    }
    fmt.Println("Listening on :8080")

    for {
        conn, err := ln.Accept()
        if err != nil {
            fmt.Println("accept error:", err)
            continue
        }
        go handleConn(conn) // each client gets its own goroutine
    }
}