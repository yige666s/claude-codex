package websandbox

import (
	"encoding/json"
	"log"
)

type AuditSink interface {
	Record(AuditEvent)
}

type LogAuditSink struct{}

func (LogAuditSink) Record(event AuditEvent) {
	payload, err := json.Marshal(event)
	if err != nil {
		log.Printf("websandbox audit marshal error: %v", err)
		return
	}
	log.Printf("websandbox audit %s", payload)
}

type NoopAuditSink struct{}

func (NoopAuditSink) Record(AuditEvent) {}
