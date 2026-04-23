package cluster

import (
	"encoding/json"
	"log"

	zmq "github.com/pebbe/zmq4"
)

// =======================
// 🌐 CONSTANTS
// =======================

const (
	ZMQRouterPort       = 5555
	ZMQPubPort          = 5556
	ZMQDiscoveryPubPort = 5557
	ZMQAsyncPort        = 5558
)

// =======================
// 📦 MESSAGE TYPES
// =======================

// Cluster transport envelope (STRICT transport only)
type ZMQMessage struct {
	Type     string                 `json:"type"`
	FromNode string                 `json:"from_node"`
	ToNode   string                 `json:"to_node"`
	Payload  map[string]interface{} `json:"payload"`
}

// SIP/WebRTC future-ready envelope
type CallRouteMessage struct {
	Type     string `json:"type"`
	CallID   string `json:"call_id"`
	FromNode string `json:"from_node"`
	ToNode   string `json:"to_node"`
	SDP      string `json:"sdp,omitempty"`
	ICE      string `json:"ice,omitempty"`
}

// =======================
// 🔌 CONFIG
// =======================

type NewZMQNodeConnectionConfig struct {
	NodeID     string
	RouterAddr string
	PubAddr    string
	DealerAddr string
	Bind       bool
}

// =======================
// 🧠 TRANSPORT CORE
// =======================

type ZMQNodeConnection struct {
	nodeID string
	ctx    *zmq.Context

	routerSocket *zmq.Socket
	pubSocket    *zmq.Socket
	subSocket    *zmq.Socket
	dealerSocket *zmq.Socket
}

// =======================
// 🚀 INIT
// =======================

func NewZMQNodeConnection(config NewZMQNodeConnectionConfig) (*ZMQNodeConnection, error) {
	ctx, err := zmq.NewContext()
	if err != nil {
		return nil, err
	}

	c := &ZMQNodeConnection{
		nodeID: config.NodeID,
		ctx:    ctx,
	}

	// ---------------- ROUTER (direct node messaging)
	if config.RouterAddr != "" {
		s, err := ctx.NewSocket(zmq.ROUTER)
		if err != nil {
			return nil, err
		}

		if config.Bind {
			err = s.Bind(config.RouterAddr)
		} else {
			err = s.Connect(config.RouterAddr)
		}
		if err != nil {
			return nil, err
		}

		c.routerSocket = s
	}

	// ---------------- PUB (cluster events)
	if config.PubAddr != "" {
		s, err := ctx.NewSocket(zmq.PUB)
		if err != nil {
			return nil, err
		}

		if config.Bind {
			err = s.Bind(config.PubAddr)
		} else {
			err = s.Connect(config.PubAddr)
		}
		if err != nil {
			return nil, err
		}

		c.pubSocket = s

		// SUB socket mirrors PUB
		sub, err := ctx.NewSocket(zmq.SUB)
		if err != nil {
			return nil, err
		}

		sub.SetSubscribe("")
		sub.Connect(config.PubAddr)

		c.subSocket = sub
	}

	// ---------------- DEALER (async relay path)
	if config.DealerAddr != "" {
		s, err := ctx.NewSocket(zmq.DEALER)
		if err != nil {
			return nil, err
		}

		if config.Bind {
			err = s.Bind(config.DealerAddr)
		} else {
			err = s.Connect(config.DealerAddr)
		}
		if err != nil {
			return nil, err
		}

		c.dealerSocket = s
	}

	log.Printf("[ZMQ] node ready %s", config.NodeID)

	return c, nil
}

// =======================
// 📡 BROADCAST (EVENT LAYER ONLY)
// =======================

func (c *ZMQNodeConnection) Broadcast(eventType string, data []byte) error {
	if c.pubSocket == nil {
		return nil
	}

	_, err := c.pubSocket.SendMessage(eventType, data)
	return err
}

// =======================
// 📡 DIRECT ROUTE (NODE → NODE)
// =======================

func (c *ZMQNodeConnection) SendMessage(toNode string, msg *ZMQMessage) error {
	if c.routerSocket == nil {
		return nil
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	// ROUTER expects: identity + payload
	_, err = c.routerSocket.SendMessage(toNode, data)
	return err
}

// =======================
// 🚀 ASYNC RELAY (ICE / SFU / future SIP media plane)
// =======================

func (c *ZMQNodeConnection) RelayAsync(msgType string, payload []byte) error {
	if c.dealerSocket == nil {
		return nil
	}

	_, err := c.dealerSocket.SendMessage(msgType, payload)
	return err
}

// =======================
// 👂 RECEIVE (used by Discovery / event loops)
// =======================

func (c *ZMQNodeConnection) Receive() ([]string, error) {
	if c.subSocket == nil {
		return nil, nil
	}

	msg, err := c.subSocket.RecvMessage(0)
	if err != nil {
		return nil, err
	}

	return msg, nil
}

// =======================
// 🧹 CLEANUP
// =======================

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

	log.Printf("[ZMQ] node closed %s", c.nodeID)
}