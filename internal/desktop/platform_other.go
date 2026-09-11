//go:build !darwin

package desktop

func PreparePlatformEnvironment()  {}
func openMacPanel(url string) bool { return false }
