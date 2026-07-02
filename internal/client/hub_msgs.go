package client

// hubServersLoadedMsg is sent when a hub server listing finishes loading.
type hubServersLoadedMsg struct {
	servers []HubServerEntry
	hubURL  string
}

// hubLoadErrorMsg is sent when a hub server listing fails.
type hubLoadErrorMsg struct {
	err    string
	hubURL string
}

// hubJoinResponseMsg is sent when a hub join request succeeds.
type hubJoinResponseMsg struct {
	resp *HubJoinResponse
}

// hubJoinVerifiedMsg is sent when the join token was successfully redeemed
// with the Concord server itself, confirming the hub referral end-to-end.
type hubJoinVerifiedMsg struct {
	resp *HubJoinResponse
}

// hubJoinErrorMsg is sent when a hub join request fails.
type hubJoinErrorMsg struct {
	err string
}

// hubHealthCheckMsg is sent when a hub health check finishes.
type hubHealthCheckMsg struct {
	hubURL  string
	hubName string
}

// hubHealthCheckErrMsg is sent when a hub health check fails.
type hubHealthCheckErrMsg struct {
	hubURL string
	err    string
}

// hubPeersLoadedMsg is sent when a hub's peer listing finishes loading.
type hubPeersLoadedMsg struct {
	peers  []HubEntry
	hubURL string
}

// hubPeersLoadErrMsg is sent when fetching hub peers fails (silently ignored).
type hubPeersLoadErrMsg struct {
	err    string
	hubURL string
}
