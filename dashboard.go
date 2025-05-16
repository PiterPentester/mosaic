package main

import _ "embed"

//go:embed dashboard.html
var dashboardHTML string

func getDashboardHTML() string {
    return dashboardHTML
}
