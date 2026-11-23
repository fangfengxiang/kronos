package main

import "os"

func Example() {
	const instant = "2025-01-02T01:00:00Z"
	for _, zoneID := range []string{"America/Los_Angeles", "Asia/Shanghai"} {
		if err := run([]string{"-zone", zoneID, instant}, os.Stdout); err != nil {
			panic(err)
		}
	}
	// Output:
	// 2025-01-01
	// 2025-01-02
}
