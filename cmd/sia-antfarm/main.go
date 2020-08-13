package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"gitlab.com/NebulousLabs/Sia-Ant-Farm/antfarm"
)

func main() {
	configPath := flag.String("config", "config.json", "path to the sia-antfarm configuration file")
	flag.Parse()

	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, os.Interrupt)

	// Read and decode the sia-antfarm configuration file.
	var antfarmConfig antfarm.AntfarmConfig
	f, err := os.Open(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening %v: %v\n", *configPath, err)
		os.Exit(1)
	}

	if err = json.NewDecoder(f).Decode(&antfarmConfig); err != nil {
		fmt.Fprintf(os.Stderr, "error decoding %v: %v\n", *configPath, err)
		os.Exit(1)
	}
	f.Close()

	farm, err := antfarm.New(antfarmConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating antfarm: %v\n", err)
		os.Exit(1)
	}
	defer farm.Close()
	go farm.ServeAPI()
	go farm.PermanentSyncMonitor()

	fmt.Printf("Finished.  Running sia-antfarm with %v ants.\n", len(antfarmConfig.AntConfigs))
	<-sigchan
	fmt.Println("Caught quit signal, quitting...")
}
