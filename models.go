package main

import "time"

type Node struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	NetworkID    string   `json:"network_id"`
	LastSeen     int64    `json:"last_seen"`
	Status       string   `json:"status"`
	Capabilities []string `json:"capabilities"`
}

func (n *Node) ComputeStatus() {
	n.Status = "offline"
	if time.Now().Unix()-n.LastSeen < 60 {
		n.Status = "online"
	}
}

type Message struct {
	Type    string                 `json:"type"`
	FromID  string                 `json:"from_id"`
	ToID    string                 `json:"to_id"`
	CallID  string                 `json:"call_id,omitempty"`
	SDP     string                 `json:"sdp,omitempty"`
	Payload map[string]interface{} `json:"payload,omitempty"`
	Time    int64                  `json:"time"`
}

const (
	MsgCallRequest  = "CALL_REQUEST"
	MsgCallAccept   = "CALL_ACCEPT"
	MsgCallReject   = "CALL_REJECT"
	MsgCallEnd      = "CALL_END"
	MsgSDPOffer     = "SDP_OFFER"
	MsgSDPAnswer    = "SDP_ANSWER"
	MsgICECandidate = "ICE_CANDIDATE"
	MsgMessage      = "MESSAGE"
)

type Call struct {
	CallID    string `json:"call_id"`
	A         string `json:"a"`
	B         string `json:"b"`
	State     string `json:"state"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

const (
	CallStateRinging = "ringing"
	CallStateActive  = "active"
	CallStateEnded   = "ended"
)

type RegisterRequest struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	NetworkID    string   `json:"network_id"`
	Capabilities []string `json:"capabilities"`
}

type SignalRequest struct {
	Type    string                 `json:"type"`
	FromID  string                 `json:"from_id"`
	ToID    string                 `json:"to_id"`
	CallID  string                 `json:"call_id,omitempty"`
	SDP     string                 `json:"sdp,omitempty"`
	Payload map[string]interface{} `json:"payload,omitempty"`
}
