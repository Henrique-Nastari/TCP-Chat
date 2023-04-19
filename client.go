package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"sync"
)

const (
	CONN_PORT = ":3333"
	CONN_TYPE = "tcp"

	MSG_DESCONECTAR = "Desconectado do servidor.\n"
)

var wg sync.WaitGroup

// Reads from the socket and outputs to the console.
func Read(conn net.Conn) {
	reader := bufio.NewReader(conn)
	for {
		str, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf(MSG_DESCONECTAR)
			wg.Done()
			return
		}
		fmt.Print(str)
	}
}

func Write(conn net.Conn) {
	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(conn)

	for {
		str, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		_, err = writer.WriteString(str)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		err = writer.Flush()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}
}

func main() {
	wg.Add(1)

	conn, err := net.Dial(CONN_TYPE, CONN_PORT)
	if err != nil {
		fmt.Println(err)
	}

	go Read(conn)
	go Write(conn)

	wg.Wait()
}