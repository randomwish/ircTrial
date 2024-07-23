package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

type client struct {
	ch     chan<- string
	name   string
	colour string
}

type User struct {
	password string
	login    bool
	colour   string
}

var colours = map[string]string{
	"Reset":   "\033[0m",
	"red":     "\033[31m",
	"green":   "\033[32m",
	"yellow":  "\033[33m",
	"blue":    "\033[34m",
	"magenta": "\033[35m",
	"cyan":    "\033[36m",
	"gray":    "\033[37m",
	"white":   "\033[97m",
}

var (
	entering = make(chan client)
	leaving  = make(chan client)
	messages = make(chan string)
	users    = make(map[string]*User)
	usersMu  sync.Mutex
)

func broadcaster() {
	clients := make(map[client]bool)
	for {
		select {
		case msg := <-messages:
			for cli := range clients {
				select {
				case cli.ch <- msg:
					// Message sent successfully
				default:
					// Channel is either full or closed, remove the client
					delete(clients, cli)
					close(cli.ch)
				}
			}
		case cli := <-entering:
			clients[cli] = true
			cli.ch <- "Current clients:"
			for c := range clients {
				cli.ch <- c.name
			}

		case cli := <-leaving:
			if _, ok := clients[cli]; ok {
				delete(clients, cli)
				close(cli.ch)
			}
		}
	}
}
func handleConn(conn net.Conn) {
	defer conn.Close()
	ch := make(chan string) // outgoing client messages
	go clientWriter(conn, ch)
	input := bufio.NewScanner(conn)
	for {
		ch <- "Welcome! Please login or register!"
		ch <- "Enter 'login <username> <password>' or 'register <username> <password>'"

		var cl client
		authenticated := false
		for !authenticated {

			if !input.Scan() {
				return
			}
			command := strings.Fields(input.Text())
			action := command[0]
			if action == "exit" {
				handleExit(ch, conn)
				return
			}
			if len(command) != 3 {
				ch <- "Invalid command. Please try again."
				continue
			}

			switch action {

			case "login", "register":
				username, password := command[1], command[2]
				authenticated, cl = handleAuthAction(action, username, password, ch)
			default:
				ch <- "Invalid command. Please use 'login' or 'register'."
			}
		}
		ch <- fmt.Sprintf("You are logged in as %s", cl.name)
		messages <- cl.name + " has arrived"
		entering <- cl
		ch <- "You may now message. Be friendly to others!"
		cl.colour = getUserColour(cl.name)
		ch <- fmt.Sprintf("Colour is %s", cl.colour)

		chatLoop(input, &cl, ch, conn)
		messages <- cl.name + " has left"
		leaving <- cl
		logoutUser(cl.name)
	}
}

func handleAuthAction(action, username, password string, ch chan<- string) (bool, client) {
	switch action {
	case "login":
		status, msg := authenticateUser(username, password)
		if status {
			ch <- "Login successful!"
			return true, client{ch: ch, name: username}
		} else {
			ch <- msg
			return false, client{}
		}
	case "register":
		if registerUser(username, password) {
			ch <- "Registration successful! You are now logged in. Your default colour is white"
			return true, client{ch: ch, name: username}
		} else {
			ch <- "Username already exists. Please choose a different username."
			return false, client{}
		}
	}
	return false, client{}
}

func chatLoop(input *bufio.Scanner, cl *client, ch chan<- string, conn net.Conn) {
	for input.Scan() {
		inputText := input.Text()
		if strings.HasPrefix(inputText, "/color ") {
			handleColorChange(inputText, cl, ch)
			continue
		}
		switch inputText {
		case "/help":
			ch <- "Available commands: /color, /help, /members, /logout"
		case "/members":
			sendUserList(ch, users)
		case "/logout":
			return
		case "/exit":
			handleExit(ch, conn)
			return
		default:
			sendColoredMessage(cl, inputText)
		}
	}
}

func handleColorChange(inputText string, cl *client, ch chan<- string) {
	rawColor := strings.TrimSpace(strings.TrimPrefix(inputText, "/color "))
	color := strings.ToLower(rawColor)
	if _, exists := colours[color]; exists {
		changeUserColour(cl.name, color)
		ch <- fmt.Sprintf("Color changed to %s", color)
		cl.colour = color
	} else {
		ch <- "Invalid color. Available colors: " + strings.Join(getColorNames(), ", ")
	}
}

func sendColoredMessage(cl *client, inputText string) {
	colorString := colours[cl.colour]
	resetString := colours["Reset"]
	coloredMessage := colorString + fmt.Sprintf("%s: %s", cl.name, inputText) + resetString
	messages <- coloredMessage
}
func getUserColour(name string) string {
	userToGet := users[name]
	return userToGet.colour
}
func changeUserColour(name string, newColour string) {
	usersMu.Lock()
	defer usersMu.Unlock()
	userToChange := users[name]
	userToChange.colour = newColour
}

func getColorNames() []string {
	colorNames := make([]string, 0, len(colours))
	for name := range colours {
		if name != "Reset" {
			colorNames = append(colorNames, name)
		}
	}
	return colorNames
}

func logoutUser(username string) {
	usersMu.Lock()
	defer usersMu.Unlock()
	if user, exists := users[username]; exists {
		user.login = false
		users[username] = user
		log.Printf("User %s logged out and status updated", username)
	} else {
		log.Printf("Attempted to logout non-existent user: %s", username)
	}
}
func authenticateUser(username, password string) (bool, string) {
	usersMu.Lock()
	defer usersMu.Unlock()
	user, exists := users[username]
	if exists && user.password == password {
		if user.login {
			return false, "Someone has already logged in to this account!"
		} else {
			user.login = true
			return true, ""
		}
	}
	return false, "Wrong username/password details."
}

func registerUser(username, password string) bool {
	usersMu.Lock()
	defer usersMu.Unlock()
	if _, exists := users[username]; exists {
		return false
	}
	users[username] = &User{password: password, colour: "white"}
	return true
}
func sendUserList(ch chan<- string, clientList map[string]*User) {
	ch <- fmt.Sprint("Current list of users:")
	for username := range clientList {
		ch <- fmt.Sprintf("%s", username)
	}
}

func clientWriter(conn net.Conn, ch <-chan string) {
	for msg := range ch {
		fmt.Fprintln(conn, msg) // NOTE: ignoring network errors
	}
}

func handleExit(ch chan<- string, conn net.Conn) {
	defer conn.Close()
	ch <- "Goodbye!"

	// Give some time for the message to be sent before closing the connection
	time.Sleep(100 * time.Millisecond)

	// Close the writer goroutine's channel
	close(ch)
	log.Printf("User has logged out and status updated")

	// Close the connection
}

func main() {
	listener, err := net.Listen("tcp", "localhost:8000")
	if err != nil {
		log.Fatal(err)
	}
	go broadcaster()
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Print(err)
			continue
		}
		go handleConn(conn)
	}
}
