package installer

// TestPlan is testPlan, for the tests in package installer_test.
var TestPlan = testPlan

// MergeServerConfig and MergeHubConfig are mergeServerConfig and mergeHubConfig.
func (pl *Plan) MergeServerConfig(hubURL string) ([]byte, error) { return pl.mergeServerConfig(hubURL) }

func (pl *Plan) MergeHubConfig(name, url string) ([]byte, error) { return pl.mergeHubConfig(name, url) }
