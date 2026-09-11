//go:build !darwin

package main

func preparePlatformEnvironment()  {}
func openMacPanel(url string) bool { return false }
