package cluster

import (
	zmq "github.com/pebbe/zmq4"
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
