package cluster

import (
	"encoding/json"
	zmq "github.com/pebbe/zmq4"
	"log"
)

const (
	ZMQRouterPort       = 5555
	ZMQPubPort          = 5556
	ZMQDiscoveryPubPort = 5557
	ZMQAsyncPort        = 5558
)

type ZMQMessage struct {
	Type     string
	FromNode string
	ToNode   string
	Payload  map[string]interface{}
}

type CallRouteMessage struct {
	Type     string `json:"type"`
	CallID   string `json:"call_id"`
	FromNode string `json:"from_node"`
	ToNode   string `json:"to_node"`
	SDP      string `json:"sdp,omitempty"`
	ICE      string `json:"ice,omitempty"`
}

type NewZMQNodeConnectionConfig struct {
	NodeID     string
	RouterAddr string
	PubAddr    string
	DealerAddr string
	Bind       bool
}

type ZMQNodeConnection struct {
	nodeID       string
	routerSocket *zmq.Socket
	pubSocket    *zmq.Socket
	subSocket    *zmq.Socket
	dealerSocket *zmq.Socket
	ctx          *zmq.Context
	peers        map[string]string
}

type ZMQCallRouter struct {
	nodeID     string
	router     *zmq.Socket
	routerAddr string
	ctx        *zmq.Context
	routes     map[string]string
	bind       bool
}

func NewZMQCallRouter(nodeID string, port int, bind bool) (*ZMQCallRouter, error) {
	ctx, err := zmq.NewContext()
	if err != nil {
		return nil, err
	}

	router, err := ctx.NewSocket(zmq.ROUTER)
	if err != nil {
		ctx.Term()
		return nil, err
	}

	addr := "tcp *:5559"
	if port > 0 {
		addr = "tcp *:" + string(rune(port+4000))
	}

	if bind {
		router.Bind(addr)
		log.Printf("[ZMQ] ROUTER bound to %s", addr)
	} else {
		router.Connect(addr)
		log.Printf("[ZMQ] ROUTER connected to %s", addr)
	}

	return &ZMQCallRouter{
		nodeID:     nodeID,
		router:     router,
		routerAddr: addr,
		ctx:        ctx,
		routes:     make(map[string]string),
		bind:       bind,
	}, nil
}

func (r *ZMQCallRouter) RegisterRoute(nodeID, remoteAddr string) {
	r.routes[nodeID] = remoteAddr
	log.Printf("[ZMQ] Registered route: %s -> %s", nodeID, remoteAddr)
}

func (r *ZMQCallRouter) RouteCall(msg *CallRouteMessage) error {
	if r.router == nil {
		return nil
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	_, err = r.router.Send(msg.ToNode, zmq.SNDMORE)
	if err != nil {
		return err
	}

	_, err = r.router.Send(string(data), 0)
	if err != nil {
		return err
	}

	log.Printf("[ZMQ] Routed %s call %s from %s to %s", msg.Type, msg.CallID, msg.FromNode, msg.ToNode)
	return nil
}

func (r *ZMQCallRouter) ReceiveCall() (*CallRouteMessage, string, error) {
	if r.router == nil {
		return nil, "", nil
	}

	msg, err := r.router.RecvMessage(0)
	if err != nil {
		return nil, "", err
	}

	if len(msg) < 2 {
		return nil, "", nil
	}

	fromNode := msg[0]
	var callMsg CallRouteMessage
	if err := json.Unmarshal([]byte(msg[1]), &callMsg); err != nil {
		return nil, fromNode, err
	}

	return &callMsg, fromNode, nil
}

func (r *ZMQCallRouter) Close() {
	if r.router != nil {
		r.router.Close()
	}
	if r.ctx != nil {
		r.ctx.Term()
	}
}

func NewZMQNodeConnection(config NewZMQNodeConnectionConfig) (*ZMQNodeConnection, error) {
	ctx, err := zmq.NewContext()
	if err != nil {
		return nil, err
	}

	conn := &ZMQNodeConnection{
		nodeID: config.NodeID,
		ctx:    ctx,
		peers:  make(map[string]string),
	}

	if config.RouterAddr != "" {
		router, err := ctx.NewSocket(zmq.ROUTER)
		if err != nil {
			ctx.Term()
			return nil, err
		}
		if config.Bind {
			router.Bind(config.RouterAddr)
		} else {
			router.Connect(config.RouterAddr)
		}
		conn.routerSocket = router
	}

	if config.PubAddr != "" {
		pub, err := ctx.NewSocket(zmq.PUB)
		if err != nil {
			ctx.Term()
			return nil, err
		}
		if config.Bind {
			pub.Bind(config.PubAddr)
		} else {
			pub.Connect(config.PubAddr)
		}
		conn.pubSocket = pub
	}

	if config.DealerAddr != "" {
		dealer, err := ctx.NewSocket(zmq.DEALER)
		if err != nil {
			ctx.Term()
			return nil, err
		}
		if config.Bind {
			dealer.Bind(config.DealerAddr)
		} else {
			dealer.Connect(config.DealerAddr)
		}
		conn.dealerSocket = dealer
	}

	return conn, nil
}

func (c *ZMQNodeConnection) AddPeer(peerID, addr string) {
	c.peers[peerID] = addr
}

func (c *ZMQNodeConnection) SendMessage(toNodeID string, msg *ZMQMessage) error {
	if c.routerSocket == nil {
		return nil
	}
	_, err := c.routerSocket.Send(toNodeID, zmq.SNDMORE)
	return err
}

func (c *ZMQNodeConnection) Broadcast(msgType string, data []byte) error {
	if c.pubSocket == nil {
		return nil
	}
	_, err := c.pubSocket.SendMessage(msgType, data)
	return err
}

func (c *ZMQNodeConnection) Receive() ([]string, error) {
	if c.routerSocket == nil {
		return nil, nil
	}
	return c.routerSocket.RecvMessage(0)
}

func (c *ZMQNodeConnection) Close() {
	if c.routerSocket != nil {
		c.routerSocket.Close()
	}
	if c.pubSocket != nil {
		c.pubSocket.Close()
	}
	if c.subSocket != nil {
		c.subSocket.Close()
	}
	if c.dealerSocket != nil {
		c.dealerSocket.Close()
	}
	if c.ctx != nil {
		c.ctx.Term()
	}
}
