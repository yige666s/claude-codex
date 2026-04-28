package agent

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func FormatAgentID(agentName, teamName string) AgentID {
	return AgentID(agentName + "@" + teamName)
}

func ParseAgentID(id AgentID) (agentName, teamName string, ok bool) {
	value := string(id)
	at := strings.Index(value, "@")
	if at < 0 {
		return "", "", false
	}
	return value[:at], value[at+1:], true
}

func GenerateRequestID(requestType string, agentID AgentID) string {
	return fmt.Sprintf("%s-%d@%s", requestType, time.Now().UnixMilli(), agentID)
}

func ParseRequestID(requestID string) (requestType string, timestamp int64, agentID AgentID, ok bool) {
	at := strings.Index(requestID, "@")
	if at < 0 {
		return "", 0, "", false
	}
	prefix := requestID[:at]
	dash := strings.LastIndex(prefix, "-")
	if dash < 0 {
		return "", 0, "", false
	}
	ts, err := strconv.ParseInt(prefix[dash+1:], 10, 64)
	if err != nil {
		return "", 0, "", false
	}
	return prefix[:dash], ts, AgentID(requestID[at+1:]), true
}
