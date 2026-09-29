package update

func LoadManifest(path string) *Release {
	return loadManifest(path)
}

func SetLatestURL(url string) {
	latestURL = url
}

func GetLatestURL() string {
	return latestURL
}

func SwapLatestURL(url string) func() {
	old := latestURL
	latestURL = url
	return func() { latestURL = old }
}
