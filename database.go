package main

import (
	"database/sql"
	"encoding/json"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Database struct {
	db *sql.DB
}

func NewDatabase(path string) (*Database, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return &Database{db: db}, nil
}

func (d *Database) Migrate() error {
	// Nodes table
	_, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS nodes (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			last_seen INTEGER NOT NULL,
			status TEXT NOT NULL,
			capabilities TEXT NOT NULL,
			created_at INTEGER NOT NULL
		)
	`)
	if err != nil {
		return err
	}

	// Calls table
	_, err = d.db.Exec(`
		CREATE TABLE IF NOT EXISTS calls (
			call_id TEXT PRIMARY KEY,
			a TEXT NOT NULL,
			b TEXT NOT NULL,
			state TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)
	`)
	if err != nil {
		return err
	}

	// Messages table
	_, err = d.db.Exec(`
		CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			to_id TEXT NOT NULL,
			from_id TEXT NOT NULL,
			type TEXT NOT NULL,
			call_id TEXT,
			payload TEXT,
			time INTEGER NOT NULL,
			delivered INTEGER DEFAULT 0
		)
	`)
	return err
}

// Node operations
func (d *Database) CreateNode(id, name string, capabilities []string) error {
	lastSeen := time.Now().Unix()
	status := "offline"
	capJSON, _ := json.Marshal(capabilities)

	_, err := d.db.Exec(
		"INSERT OR REPLACE INTO nodes (id, name, last_seen, status, capabilities, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		id, name, lastSeen, status, string(capJSON), lastSeen,
	)
	return err
}

func (d *Database) UpdateNodeLastSeen(id string) error {
	now := time.Now().Unix()
	status := "online"
	if now%2 == 0 { // placeholder logic - will be overridden by ComputeStatus
		status = "online"
	}

	_, err := d.db.Exec(
		"UPDATE nodes SET last_seen = ?, status = ? WHERE id = ?",
		now, status, id,
	)
	return err
}

func (d *Database) GetNode(id string) (*Node, error) {
	row := d.db.QueryRow("SELECT id, name, last_seen, status, capabilities FROM nodes WHERE id = ?", id)

	var node Node
	var capsJSON string
	err := row.Scan(&node.ID, &node.Name, &node.LastSeen, &node.Status, &capsJSON)
	if err != nil {
		return nil, err
	}

	json.Unmarshal([]byte(capsJSON), &node.Capabilities)
	return &node, nil
}

func (d *Database) GetAllNodes() ([]*Node, error) {
	rows, err := d.db.Query("SELECT id, name, last_seen, status, capabilities FROM nodes ORDER BY last_seen DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*Node
	for rows.Next() {
		var node Node
		var capsJSON string
		err := rows.Scan(&node.ID, &node.Name, &node.LastSeen, &node.Status, &capsJSON)
		if err != nil {
			continue
		}
		json.Unmarshal([]byte(capsJSON), &node.Capabilities)
		nodes = append(nodes, &node)
	}
	return nodes, nil
}

// Call operations
func (d *Database) CreateCall(callID, a, b string) error {
	now := time.Now().Unix()
	_, err := d.db.Exec(
		"INSERT INTO calls (call_id, a, b, state, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		callID, a, b, "ringing", now, now,
	)
	return err
}

func (d *Database) UpdateCallState(callID, state string) error {
	now := time.Now().Unix()
	_, err := d.db.Exec(
		"UPDATE calls SET state = ?, updated_at = ? WHERE call_id = ?",
		state, now, callID,
	)
	return err
}

func (d *Database) GetCall(callID string) (*Call, error) {
	row := d.db.QueryRow("SELECT call_id, a, b, state, created_at, updated_at FROM calls WHERE call_id = ?", callID)

	var call Call
	err := row.Scan(&call.CallID, &call.A, &call.B, &call.State, &call.CreatedAt, &call.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &call, nil
}

// GetAllCalls returns all calls
func (d *Database) GetAllCalls() ([]*Call, error) {
	rows, err := d.db.Query("SELECT call_id, a, b, state, created_at, updated_at FROM calls")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var calls []*Call
	for rows.Next() {
		var call Call
		err := rows.Scan(&call.CallID, &call.A, &call.B, &call.State, &call.CreatedAt, &call.UpdatedAt)
		if err != nil {
			continue
		}
		calls = append(calls, &call)
	}
	return calls, nil
}


// GetAllCalls returns all calls

// Message operations
func (d *Database) QueueMessage(msg Message) error {
	payloadJSON, _ := json.Marshal(msg.Payload)
	_, err := d.db.Exec(
		"INSERT INTO messages (to_id, from_id, type, call_id, payload, time, delivered) VALUES (?, ?, ?, ?, ?, ?, 0)",
		msg.ToID, msg.FromID, msg.Type, msg.CallID, string(payloadJSON), msg.Time,
	)
	return err
}

func (d *Database) GetAndMarkMessages(nodeID string) ([]Message, error) {
	rows, err := d.db.Query(
		"SELECT id, from_id, to_id, type, call_id, payload, time FROM messages WHERE to_id = ? AND delivered = 0",
		nodeID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var msg Message
		var id int64
		var payloadJSON string
		err := rows.Scan(&id, &msg.FromID, &msg.ToID, &msg.Type, &msg.CallID, &payloadJSON, &msg.Time)
		if err != nil {
			continue
		}
		json.Unmarshal([]byte(payloadJSON), &msg.Payload)

		// Mark as delivered
		_, _ = d.db.Exec("UPDATE messages SET delivered = 1 WHERE id = ?", id)
		msgs = append(msgs, msg)
	}
	return msgs, nil
}

// Cleanup old messages and calls
func (d *Database) Cleanup(olderThanSeconds int64) error {
	cutoff := time.Now().Unix() - olderThanSeconds

	// Clean delivered messages older than cutoff
	_, err := d.db.Exec("DELETE FROM messages WHERE delivered = 1 AND time < ?", cutoff)
	if err != nil {
		return err
	}

	// Clean ended calls older than cutoff
	_, err = d.db.Exec("DELETE FROM calls WHERE state = 'ended' AND updated_at < ?", cutoff)
	return err
}

// Close database connection
func (d *Database) Close() error {
	return d.db.Close()
}
