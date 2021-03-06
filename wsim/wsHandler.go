package wsim

import (
	"container/list"
	// "log"
	"net/url"
	"strings"

	"github.com/Eric-GreenComb/ws-im-server/types"
)

// WsMessage WsMessage
type WsMessage struct {
	//message raw data
	message string

	//message command type
	command string
	//message headers
	Headers map[string]string
	//message body
	body string
}

//fill message by command headers and body
func (m *WsMessage) serializeMessage() string {
	m.message = types.ProtocolNameWithVersion + " "
	m.message += m.command + types.CRLF

	for k, v := range m.Headers {
		m.message += k + ":" + v + types.CRLF
	}
	m.message += types.CRLF + m.body

	return m.message
}

// BuildMessage parse websocket body
func BuildMessage(data string) *WsMessage {
	//TODO optimise ,to use builder pattern
	s := data
	message := &WsMessage{message: data}
	message.Headers = make(map[string]string, types.HeaderMax)
	//parse message

	//parse start line
	i := strings.Index(s, types.CRLF)
	message.command = s[types.ProtocolLength+1 : i]

	//parse hearders
	k := 0
	headers := s[i+2:]
	var key string
	var value string
	//traverse once
	length := len(headers)
	for j, ch := range headers {
		if ch == ':' && key == "" {
			key = headers[k:j]
			k = j + 1
		} else if length > j+1 && headers[j:j+2] == types.CRLF {
			value = headers[k:j]
			k = j + 2

			message.Headers[key] = value
			// log.Print("parse head key:", key, " value:", value)
			key = ""
		}
		if length > k+1 && headers[k:k+2] == types.CRLF {
			k += 2
			break
		}
	}

	//set body
	message.body = headers[k:]

	return message
}

// WsHandler WsHandler
type WsHandler struct {
	callbacks HandlerCallbacks

	//websocket connection
	conn *Conn

	// nats conn
	SubscribeNatsConn map[string]interface{}

	//receive message
	message *WsMessage

	resp WsMessage

	upstreamURL *url.URL
	//one connection set id map sevel connections
	//connSetID string

	//save multipars datas, it is a list
	multiparts *MultipartBlock
}

func (req *WsHandler) reset() {
	req.resp = WsMessage{command: "", Headers: nil, body: ""}
}

// GetMultipart GetMultipart
func (req *WsHandler) GetMultipart() *MultipartBlock {
	return req.multiparts
}

//define subscribe callback as a WsHandler method is very very very importent
func (req *WsHandler) subscribeCallback(s string) {
	Message.Send(req.conn, s)
}

// SetCommand SetCommand
func (req *WsHandler) SetCommand(s string) {
	req.resp.command = s
}

// GetCommand GetCommand
func (req *WsHandler) GetCommand() string {
	return req.message.command
}

// SetHeader SetHeader
func (req *WsHandler) SetHeader(hkey, hvalue string) {
	req.message.Headers[hkey] = hvalue
}

// GetHeader GetHeader
func (req *WsHandler) GetHeader(hkey string) string {
	if value, ok := req.message.Headers[hkey]; ok {
		return value
	}
	return ""
}

// AddHeader if header already exist,update it
func (req *WsHandler) AddHeader(hkey, hvalue string) {
	req.resp.Headers = make(map[string]string, types.HeaderMax)
	req.resp.Headers[hkey] = hvalue
}

// GetBody GetBody
func (req *WsHandler) GetBody() string {
	return req.message.body
}

//if response is nil, use request to fill it
func (req *WsHandler) setResponse() {
	if req.resp.command == "" {
		req.resp.command = req.message.command
	}
	if req.resp.Headers == nil {
		req.resp.Headers = req.message.Headers
	}
	if req.resp.body == "" {
		req.resp.body = req.message.body
	}
}

// Send if you want change command or header ,using SetCommand or AddHeader
func (req *WsHandler) Send(body string) {
	resp := types.ProtocolNameWithVersion + " "
	if req.resp.command != "" {
		resp = resp + req.resp.command + types.CRLF
	} else {
		resp = resp + req.message.command + types.CRLF
	}

	if req.resp.Headers != nil {
		for k, v := range req.resp.Headers {
			resp = resp + k + ":" + v + types.CRLF
		}
	} else {
		for k, v := range req.message.Headers {
			resp = resp + k + ":" + v + types.CRLF
		}
	}
	resp += types.CRLF + body

	req.resp.message = resp

	// log.Print("send message:", string(req.message.message))

	Message.Send(req.conn, req.resp.message)

	req.resp = WsMessage{command: "", Headers: nil, body: ""}
}

// HandlerCallbacks HandlerCallbacks
type HandlerCallbacks interface {
	OnOpen(*WsHandler)
	OnClose(*WsHandler)
	OnMessage(*WsHandler)
}

// BaseProcessor BaseProcessor
type BaseProcessor struct {
}

// OnOpen OnOpen
func (*BaseProcessor) OnOpen(*WsHandler) {
	// log.Print("base on open")
}

// OnMessage OnMessage
func (*BaseProcessor) OnMessage(*WsHandler) {
	// log.Print("base on message")
}

// OnClose OnClose
func (*BaseProcessor) OnClose(*WsHandler) {
	// log.Print("base on close")
}

// StartServer StartServer
func StartServer(ws *Conn) {
	openFlag := 0

	//init WsHandler,set connection and connsetid
	wsHandler := &WsHandler{conn: ws}
	wsHandler.SubscribeNatsConn = make(map[string]interface{}, types.SubscribeMax)

	for {
		var data string
		err := Message.Receive(ws, &data)
		// log.Print("receive message:", string(data))
		if err != nil {
			break
		}
		l := len(data)
		if l <= types.ProtocolLength {
			//TODO how to provide other protocol
			// log.Print("TODO provide other protocol")
			continue
		}

		l = len(data)
		if l > types.MaxLength {
			//TODO how to provide other protocol
			// log.Print("TODO provide other protocol")
			continue
		}

		if data[:types.ProtocolLength] != types.ProtocolNameWithVersion {
			//TODO how to provide other protocol
			// log.Print("TODO provide other protocol")
			continue
		}

		wsHandler.message = BuildMessage(data)
		wsHandler.reset()

		var e *list.Element
		//head filter before process message
		for e = beforeRequestFilterList.Front(); e != nil; e = e.Next() {
			e.Value.(HeadFilterHandler).BeforeRequestFilterHandle(wsHandler)
		}

		wsHandler.callbacks = getProcessor(wsHandler.message.command)
		if wsHandler.callbacks == nil {
			wsHandler.callbacks = &BaseProcessor{}
		}
		// log.Print("callbacks:", wsHandler.callbacks.OnMessage)
		//just call once
		if openFlag == 0 {
			for e = onOpenFilterList.Front(); e != nil; e = e.Next() {
				e.Value.(HeadFilterHandler).OnOpenFilterHandle(wsHandler)
			}
			if wsHandler.callbacks != nil {
				wsHandler.callbacks.OnOpen(wsHandler)
			} else {
				//log.Print("error on open is null")
			}
			openFlag = 1
		}
		if wsHandler.callbacks != nil {
			wsHandler.callbacks.OnMessage(wsHandler)
		} else {
			//log.Print("error onmessage is null ")
		}

		//head filter after process message
		for e = afterRequestFilterList.Front(); e != nil; e = e.Next() {
			e.Value.(HeadFilterHandler).AfterRequestFilterHandle(wsHandler)
		}
	}
	defer func() {
		for e := onCloseFilterList.Front(); e != nil; e = e.Next() {
			e.Value.(HeadFilterHandler).OnCloseFilterHandle(wsHandler)
		}
		if wsHandler.callbacks != nil {
			wsHandler.callbacks.OnClose(wsHandler)
		} else {
			//log.Print("error on close is null")
		}
		ws.Close()
	}()
}
