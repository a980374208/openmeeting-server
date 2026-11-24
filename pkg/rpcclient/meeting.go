package rpcclient

import (
	"context"
	"github.com/openimsdk/openmeeting-server/pkg/agentrpc"
	"github.com/openimsdk/protocol/openmeeting/meeting"
	"github.com/openimsdk/tools/discovery"
	"github.com/openimsdk/tools/system/program"
	"google.golang.org/grpc"
)

type Meeting struct {
	conn        grpc.ClientConnInterface
	Client      meeting.MeetingServiceClient
	AgentClient agentrpc.AgentTokenServiceClient
	Discovery   discovery.SvcDiscoveryRegistry
}

// NewMeeting initializes and returns a Meeting RPC client wrapper.
func NewMeeting(discovery discovery.SvcDiscoveryRegistry, rpcRegisterName string) *Meeting {
	conn, err := discovery.GetConn(context.Background(), rpcRegisterName)
	if err != nil {
		program.ExitWithError(err)
	}
	client := meeting.NewMeetingServiceClient(conn)
	agentClient := agentrpc.NewAgentTokenServiceClient(conn)
	return &Meeting{
		Discovery:   discovery,
		Client:      client,
		AgentClient: agentClient,
		conn:        conn,
	}
}
