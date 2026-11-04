// Command meshsim runs the network simulations.
//
//	go run ./sim/cmd/meshsim                 # all scenarios
//	go run ./sim/cmd/meshsim -s blackout -seed 7
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Aryalleh/mesh-msg/sim"
)

func main() {
	name := flag.String("s", "", "scenario name (default: all)")
	seed := flag.Int64("seed", 1, "random seed")
	list := flag.Bool("list", false, "list scenarios")
	flag.Parse()
	if *list {
		for _, s := range sim.Scenarios {
			fmt.Printf("%-20s %s\n", s.Name, s.Desc)
		}
		return
	}
	ran := false
	for _, s := range sim.Scenarios {
		if *name != "" && s.Name != *name {
			continue
		}
		_, report := sim.Run(s, *seed)
		fmt.Println(report)
		ran = true
	}
	if !ran {
		fmt.Fprintf(os.Stderr, "unknown scenario %q (use -list)\n", *name)
		os.Exit(2)
	}
}
