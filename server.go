package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"
)

const (
	CONN_PORT = ":3333"
	CONN_TYPE = "tcp"
	MAX_CLIENTS = 10
	CMD_PREFIX = "/"
	CMD_CRIAR = CMD_PREFIX + "criar"
	CMD_LISTA   = CMD_PREFIX + "lista"
	CMD_JUNTAR   = CMD_PREFIX + "juntar"
	CMD_DEIXAR  = CMD_PREFIX + "deixar"
	CMD_AJUDA   = CMD_PREFIX + "ajuda"
	CMD_NOME   = CMD_PREFIX + "nome"
	CMD_SAIR   = CMD_PREFIX + "sair"

	CLIENT_NAME = "Cliente"
	SERVER_NAME = "Servidor"

	ERROR_PREFIX = "Erro: "
	ERROR_ENVIAR   = ERROR_PREFIX + "Voce nao pode enviar mensagens neste servidor.\n"
	ERROR_CRIAR = ERROR_PREFIX + "Um chat com este nome ja existe.\n"
	ERROR_JUNTAR   = ERROR_PREFIX + "Um chat com este nome nao existe.\n"
	ERROR_DEIXAR  = ERROR_PREFIX + "Voce nao pode deixar este chat.\n"
	NOTICE_PREFIX          = "Alerta: "
	NOTICE_ROOM_JUNTAR       = NOTICE_PREFIX + "\"%s\" Se juntar ao chat.\n"
	NOTICE_ROOM_DEIXAR      = NOTICE_PREFIX + "\"%s\" Deixar o chat.\n"
	NOTICE_ROOM_NOME       = NOTICE_PREFIX + "\"%s\" Mudar o nome \"%s\".\n"
	NOTICE_ROOM_DELETAR     = NOTICE_PREFIX + "O chat esta inativo e sera deletado.\n"
	NOTICE_PERSONAL_CRIAR = NOTICE_PREFIX + "Criar sala de chat \"%s\".\n"
	NOTICE_PERSONAL_NOME   = NOTICE_PREFIX + "Mudar o nome para \"\".\n"
	MSG_CONECTAR = "Bem vindo ao servidor! Digite \"/ajuda\" para acessar a lista de comandos.\n"
	MSG_CHEIO    = "O servidor esta cheio. Por favor tente se conectar mais tarde."
	EXPIRY_TIME time.Duration = 7 * 24 * time.Hour
)

type Lobby struct {
	clients   []*Client
	chatRooms map[string]*ChatRoom
	incoming  chan *Message
	join      chan *Client
	leave     chan *Client
	delete    chan *ChatRoom
}

func NewLobby() *Lobby {
	lobby := &Lobby{
		clients:   make([]*Client, 0),
		chatRooms: make(map[string]*ChatRoom),
		incoming:  make(chan *Message),
		join:      make(chan *Client),
		leave:     make(chan *Client),
		delete:    make(chan *ChatRoom),
	}
	lobby.Listen()
	return lobby
}

func (lobby *Lobby) Listen() {
	go func() {
		for {
			select {
			case message := <-lobby.incoming:
				lobby.Parse(message)
			case client := <-lobby.join:
				lobby.Join(client)
			case client := <-lobby.leave:
				lobby.Leave(client)
			case chatRoom := <-lobby.delete:
				lobby.DeleteChatRoom(chatRoom)
			}
		}
	}()
}

func (lobby *Lobby) Join(client *Client) {
	if len(lobby.clients) >= MAX_CLIENTS {
		client.Quit()
		return
	}
	lobby.clients = append(lobby.clients, client)
	client.outgoing <- MSG_CONECTAR
	go func() {
		for message := range client.incoming {
			lobby.incoming <- message
		}
		lobby.leave <- client
	}()
}

func (lobby *Lobby) Leave(client *Client) {
	if client.chatRoom != nil {
		client.chatRoom.Leave(client)
	}
	for i, otherClient := range lobby.clients {
		if client == otherClient {
			lobby.clients = append(lobby.clients[:i], lobby.clients[i+1:]...)
			break
		}
	}
	close(client.outgoing)
	log.Println("Canal de saida do cliente fechado")
}

func (lobby *Lobby) DeleteChatRoom(chatRoom *ChatRoom) {
	if chatRoom.expiry.After(time.Now()) {
		go func() {
			time.Sleep(chatRoom.expiry.Sub(time.Now()))
			lobby.delete <- chatRoom
		}()
		log.Println("Tentativa de deletar a sala de chat")
	} else {
		chatRoom.Delete()
		delete(lobby.chatRooms, chatRoom.name)
		log.Println("Sala de chat deletada")
	}
}

func (lobby *Lobby) Parse(message *Message) {
	switch {
	default:
		lobby.SendMessage(message)
	case strings.HasPrefix(message.text, CMD_CRIAR):
		name := strings.TrimSuffix(strings.TrimPrefix(message.text, CMD_CRIAR+" "), "\n")
		lobby.CreateChatRoom(message.client, name)
	case strings.HasPrefix(message.text, CMD_LISTA):
		lobby.ListChatRooms(message.client)
	case strings.HasPrefix(message.text, CMD_JUNTAR):
		name := strings.TrimSuffix(strings.TrimPrefix(message.text, CMD_JUNTAR+" "), "\n")
		lobby.JoinChatRoom(message.client, name)
	case strings.HasPrefix(message.text, CMD_DEIXAR):
		lobby.LeaveChatRoom(message.client)
	case strings.HasPrefix(message.text, CMD_NOME):
		name := strings.TrimSuffix(strings.TrimPrefix(message.text, CMD_NOME+" "), "\n")
		lobby.ChangeName(message.client, name)
	case strings.HasPrefix(message.text, CMD_AJUDA):
		lobby.Help(message.client)
	case strings.HasPrefix(message.text, CMD_SAIR):
		message.client.Quit()
	}
}

func (lobby *Lobby) SendMessage(message *Message) {
	if message.client.chatRoom == nil {
		message.client.outgoing <- ERROR_ENVIAR
		log.Println("Cliente tentou enviar uma mensagem na sala.")
		return
	}
	message.client.chatRoom.Broadcast(message.String())
	log.Println("O cliente enviou uma mensagem")
}

func (lobby *Lobby) CreateChatRoom(client *Client, name string) {
	if lobby.chatRooms[name] != nil {
		client.outgoing <- ERROR_CRIAR
		log.Println("Cliente tentou criar uma sala de chat com um nome ja existente")
		return
	}
	chatRoom := NewChatRoom(name)
	lobby.chatRooms[name] = chatRoom
	go func() {
		time.Sleep(EXPIRY_TIME)
		lobby.delete <- chatRoom
	}()
	client.outgoing <- fmt.Sprintf(NOTICE_PERSONAL_CRIAR, chatRoom.name)
	log.Println("Cliente criou uma sala de chat.")
}

func (lobby *Lobby) JoinChatRoom(client *Client, name string) {
	if lobby.chatRooms[name] == nil {
		client.outgoing <- ERROR_JUNTAR
		log.Println("Cliente tentou se juntar a uma sala de chat nao existente.")
		return
	}
	if client.chatRoom != nil {
		lobby.LeaveChatRoom(client)
	}
	lobby.chatRooms[name].Join(client)
	log.Println("Cliente se juntou a uma sala de chat.")
}

func (lobby *Lobby) LeaveChatRoom(client *Client) {
	if client.chatRoom == nil {
		client.outgoing <- ERROR_DEIXAR
		log.Println("Cliente tentou deixar a sala.")
		return
	}
	client.chatRoom.Leave(client)
	log.Println("Cliente deixou a sala")
}

func (lobby *Lobby) ChangeName(client *Client, name string) {
	if client.chatRoom == nil {
		client.outgoing <- fmt.Sprintf(NOTICE_PERSONAL_NOME, name)
	} else {
		client.chatRoom.Broadcast(fmt.Sprintf(NOTICE_ROOM_NOME, client.name, name))
	}
	client.name = name
	log.Println("O cliente mudou seu nome.")
}

func (lobby *Lobby) ListChatRooms(client *Client) {
	client.outgoing <- "\n"
	client.outgoing <- "Chat Rooms:\n"
	for name := range lobby.chatRooms {
		client.outgoing <- fmt.Sprintf("%s\n", name)
	}
	client.outgoing <- "\n"
	log.Println("O cliente listou as salas.")
}

func (lobby *Lobby) Help(client *Client) {
	client.outgoing <- "\n"
	client.outgoing <- "Commandos:\n"
	client.outgoing <- "/ajuda - lista todos os comandos\n"
	client.outgoing <- "/lista - lista todas as salas\n"
	client.outgoing <- "/criar foo - cria uma sala com o nome foo\n"
	client.outgoing <- "/juntar a foo - se junta a um chat chamado foo\n"
	client.outgoing <- "/deixar - deixa a sala atual\n"
	client.outgoing <- "/nome foo - altera seu nome para foo\n"
	client.outgoing <- "/sair - sair do programa\n"
	client.outgoing <- "\n"
	log.Println("cliente solicita pedido de ajuda.")
}

type ChatRoom struct {
	name     string
	clients  []*Client
	messages []string
	expiry   time.Time
}

func NewChatRoom(name string) *ChatRoom {
	return &ChatRoom{
		name:     name,
		clients:  make([]*Client, 0),
		messages: make([]string, 0),
		expiry:   time.Now().Add(EXPIRY_TIME),
	}
}

func (chatRoom *ChatRoom) Join(client *Client) {
	client.chatRoom = chatRoom
	for _, message := range chatRoom.messages {
		client.outgoing <- message
	}
	chatRoom.clients = append(chatRoom.clients, client)
	chatRoom.Broadcast(fmt.Sprintf(NOTICE_ROOM_JUNTAR, client.name))
}

func (chatRoom *ChatRoom) Leave(client *Client) {
	chatRoom.Broadcast(fmt.Sprintf(NOTICE_ROOM_DEIXAR, client.name))
	for i, otherClient := range chatRoom.clients {
		if client == otherClient {
			chatRoom.clients = append(chatRoom.clients[:i], chatRoom.clients[i+1:]...)
			break
		}
	}
	client.chatRoom = nil
}

func (chatRoom *ChatRoom) Broadcast(message string) {
	chatRoom.expiry = time.Now().Add(EXPIRY_TIME)
	chatRoom.messages = append(chatRoom.messages, message)
	for _, client := range chatRoom.clients {
		client.outgoing <- message
	}
}


func (chatRoom *ChatRoom) Delete() {
	chatRoom.Broadcast(NOTICE_ROOM_DELETAR)
	for _, client := range chatRoom.clients {
		client.chatRoom = nil
	}
}

type Client struct {
	name     string
	chatRoom *ChatRoom
	incoming chan *Message
	outgoing chan string
	conn     net.Conn
	reader   *bufio.Reader
	writer   *bufio.Writer
}

func NewClient(conn net.Conn) *Client {
	writer := bufio.NewWriter(conn)
	reader := bufio.NewReader(conn)

	client := &Client{
		name:     CLIENT_NAME,
		chatRoom: nil,
		incoming: make(chan *Message),
		outgoing: make(chan string),
		conn:     conn,
		reader:   reader,
		writer:   writer,
	}

	client.Listen()
	return client
}

func (client *Client) Listen() {
	go client.Read()
	go client.Write()
}

func (client *Client) Read() {
	for {
		str, err := client.reader.ReadString('\n')
		if err != nil {
			log.Println(err)
			break
		}
		message := NewMessage(time.Now(), client, strings.TrimSuffix(str, "\n"))
		client.incoming <- message
	}
	close(client.incoming)
	log.Println("Tópico para leitura do canal de entrada do cliente fechado")
}

func (client *Client) Write() {
	for str := range client.outgoing {
		_, err := client.writer.WriteString(str)
		if err != nil {
			log.Println(err)
			break
		}
		err = client.writer.Flush()
		if err != nil {
			log.Println(err)
			break
		}
	}
	log.Println("Tópico de gravação do cliente fechado")
}

func (client *Client) Quit() {
	client.conn.Close()
}

type Message struct {
	time   time.Time
	client *Client
	text   string
}

func NewMessage(time time.Time, client *Client, text string) *Message {
	return &Message{
		time:   time,
		client: client,
		text:   text,
	}
}

func (message *Message) String() string {
	return fmt.Sprintf("%s - %s: %s\n", message.time.Format(time.Kitchen), message.client.name, message.text)
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	lobby := NewLobby()

	listener, err := net.Listen(CONN_TYPE, CONN_PORT)
	if err != nil {
		log.Println("Error: ", err)
		os.Exit(1)
	}
	defer listener.Close()
	log.Println("esperando em " + CONN_PORT)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("Error: ", err)
			continue
		}
		lobby.Join(NewClient(conn))
	}
}