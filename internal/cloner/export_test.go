package cloner

// These aliases give the black-box tests in package cloner_test the URL
// helpers, which have no exported caller, without widening the package API.
var (
	ResolveURL         = resolveURL
	ResolveWikiURL     = resolveWikiURL
	AuthenticatedHTTPS = authenticatedHTTPS
)
