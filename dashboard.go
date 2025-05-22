// Package main contains the web interface components for the network monitoring dashboard.
// It serves the HTML, CSS, and JavaScript for the real-time monitoring interface.
package main

import _ "embed"

// dashboardHTML contains the embedded HTML, CSS, and JavaScript for the dashboard.
// It's embedded at compile time using the go:embed directive.
//
// The dashboard provides a visual representation of host statuses with color-coded tiles:
// - Green: Host is up with good latency (or 0% packet loss)
// - Yellow: Host is up but with high latency (or some packet loss)
// - Red: Host is down (or has high packet loss)
//
//go:embed dashboard.html
var dashboardHTML string

// getDashboardHTML returns the embedded HTML content for the dashboard.
//
// Returns:
//   - string: The complete HTML, CSS, and JavaScript for the dashboard
func getDashboardHTML() string {
    return dashboardHTML
}
