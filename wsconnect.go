package main

import (
	"log"
	"net/http"
	"os"
	"sync"

	"fmt"

	"encoding/json"

	"net/url"

	"time"

	"github.com/Gorilla/mux"
	"github.com/Gorilla/websocket"
	"github.com/urfave/negroni"
)

// User is the main struct that encompasses all things needed to interact with the API
type user struct {
	conn *websocket.Conn
	resp *http.Response
	send chan []interface{}

	sync.Mutex
}

type config struct {
	Ticket            string
	PlayerNetID       string
	ClientGameVersion string
}

var client *user
var counter = 1000000
var cb = make(map[int](chan interface{}))

func readConfig() (*config, error) {
	file, _ := os.Open("config.json")
	conf := &config{}
	err := json.NewDecoder(file).Decode(&conf)
	if err != nil {
		return nil, fmt.Errorf("[ERROR]: Could not read config file `config.json` in root directory")
	}
	return conf, nil
}

func buildConnectURL() (string, error) {
	conf, err := readConfig()
	if err != nil {
		return "", err
	}

	queryVals := url.Values{}
	queryVals.Add("provider", "steam")
	queryVals.Add("ticket", conf.Ticket)
	queryVals.Add("playerNetId", conf.PlayerNetID)
	queryVals.Add("cc", "RU")
	queryVals.Add("clientGameVersion", conf.ClientGameVersion)
	u := url.URL{Scheme: "ws", Host: "entry.playbattlegrounds.com:81", Path: "/userproxy", RawQuery: queryVals.Encode()}
	return u.String(), nil
}

// ConnectToServer will connect to the websocket server, if there is an error, it will not auto-retry and the program will fail
// you can change this behaviour by adding true to the auto-retry
func connectToServer() (*user, error) {
	done := make(chan bool)

	conString, err := buildConnectURL()
	if err != nil {
		return nil, err
	}

	dial := websocket.Dialer{
		ReadBufferSize:  99999,
		WriteBufferSize: 99999,
	}

	c, _, err := dial.Dial(conString, nil)
	if err != nil {
		return nil, err
	}
	c.SetReadDeadline(time.Time{})
	c.SetWriteDeadline(time.Time{})
	log.Println("Connected to PUBG Websocket Server")

	u := &user{conn: c, resp: nil, send: make(chan []interface{}, 256)}

	go u.readSocket(done)
	go u.writeSocket(done)

	return u, nil
}

func (u *user) sendMessage(args ...interface{}) interface{} {
	u.Lock()
	counter++
	num := counter
	u.Unlock()

	sendArgs := append([]interface{}{num, nil, "UserProxyApi"}, args...)
	u.send <- sendArgs
	cb[num*-1] = make(chan interface{})
	return <-cb[num*-1]
}

func (u *user) readSocket(done chan bool) {
	defer func() {
		close(done)
		log.Println("Read Socket Closed")
	}()
	for {
		_, message, err := u.conn.ReadMessage()
		if err != nil {
			log.Println("read: ", err)
			return
		}
		go func() {
			type RawMsg []interface{}
			rawMsg := RawMsg{}
			err = json.Unmarshal(message, &rawMsg)
			if err != nil {
				log.Println("Error Unmarshalling", err)
			}
			num := int(rawMsg[0].(float64))
			if _, ok := cb[num]; ok {
				cb[int(rawMsg[0].(float64))] <- rawMsg
			}
		}()
	}
}

func (u *user) writeSocket(done chan bool) {
	// interrupt := make(chan os.Signal, 1)
	// signal.Notify(interrupt, os.Interrupt)
	ticker := time.NewTicker(time.Second * 10)

	defer func() {
		ticker.Stop()
		log.Println("Write Socket Closed")
	}()
	for {
		select {
		case <-ticker.C:
			log.Println("Heartbeat")
			err := u.conn.WriteMessage(websocket.PingMessage, []byte{})
			if err != nil {
				log.Println("write:", err)
				return
			}

		case msg := <-u.send:
			err := u.conn.WriteJSON(msg)
			if err != nil {
				log.Println(err)
			}

			// case <-interrupt:
			// 	log.Println("Interrupt")
			// 	// To cleanly close a connection, client should send a close
			// 	// frame and wait for the server to close the connection
			// 	err := u.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			// 	if err != nil {
			// 		log.Println("write close:", err)
			// 		return
			// 	}
			// 	select {
			// 	case <-done:
			// 	case <-time.After(time.Second):
			// 	}
			// 	log.Println("Returning")
			// 	u.conn.Close()
			// 	return
		}
	}
}

func main() {
	user, err := connectToServer()
	client = user
	if err != nil {
		log.Println(err)
	}

	router := mux.NewRouter()
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		val := client.sendMessage("GetBroLeaderboard", "na", "solo", "Rating", "account.59e4ce452ac94e27b02a37ac7a301135")
		log.Println("Return Value: ", val)
	})
	n := negroni.New()
	n.Use(negroni.NewLogger())
	n.UseHandler(router)

	http.ListenAndServe(":3000", n)
}
