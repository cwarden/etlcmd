package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/codegangsta/cli"
	"github.com/dailyburn/ratchet"
	"github.com/dailyburn/ratchet/logger"
	"github.com/dailyburn/ratchet/processors"

	procs "github.com/cwarden/ratchet_processors"
	"github.com/cwarden/ratchetforce"
)

const CMDNAME = "etlcmd"
const VERSION = "0.2.1"
const AUTHOR = "Sam Hug"

func main() {

	var configPath string

	app := cli.NewApp()
	app.Name = CMDNAME
	app.Usage = "A utility to assist with the automation of ETL tasks."
	app.Author = AUTHOR
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:        "config, c",
			Usage:       "Path to configuration file",
			Destination: &configPath,
		},
	}
	app.Action = func(c *cli.Context) error {

		fmt.Fprintf(os.Stderr, "%s v%s by %s\n\n", CMDNAME, VERSION, AUTHOR)

		if configPath == "" {
			log.Fatalf("You must specifiy a configuration file.\n")
		}

		config, err := LoadConfig(configPath)
		if err != nil {
			log.Fatalf("Unable to load configuration: %s\n", err)
		}

		return runApp(config)
	}

	app.Run(os.Args)
}

// Get handle to input file defined by path attribute, if defined, or stdin
func inputFile(config ConfigMap) (f *os.File) {
	if config["path"] != nil {
		var err error
		path := config["path"].(string)
		f, err = os.Open(path)
		if err != nil {
			log.Fatalf("Unable to open input file (%s): %s", path, err)
		}
	} else {
		f = os.Stdin
	}
	return
}

// Get handle to output file defined by path attribute, if defined, or stdout
func outputFile(config ConfigMap) (f *os.File) {
	if config["path"] != nil {
		var err error
		path := config["path"].(string)
		f, err = os.Create(path)
		if err != nil {
			log.Fatalf("Unable to create output file (%s): %s", path, err)
		}
	} else {
		f = os.Stdout
	}
	return
}

func runApp(config *Config) error {

	logger.LogLevel = logger.LevelError

	// Initialize ETL's
	for _, processInfo := range config.Processes {

		log.Printf("%s ETL Process", processInfo.Name)

		var processorChain []ratchet.DataProcessor
		var err error

		// Initialize Input
		var input ratchet.DataProcessor
		inputType := strings.ToLower(processInfo.Input.Type)
		inputConfig := processInfo.Input.Config

		log.Printf("  Initializing %s input", inputType)

		switch inputType {
		default:
			log.Fatalf("Unsupported input type (%s)\n", inputType)
		case "csv":
			f := inputFile(inputConfig)
			defer f.Close()

			input, err = procs.NewCSVReader(f)
			if err != nil {
				log.Fatalf("Error initializing input: %s\n", err)
			}
		case "json":
			f := inputFile(inputConfig)
			defer f.Close()

			input = procs.NewJSONReader(f)

		case "unidata":
			c := &procs.UdtConfig{}

			c.Address = config.Unidata.Host
			c.Username = config.Unidata.Username
			c.Password = config.Unidata.Password

			queryField, ok := inputConfig["query"]
			if !ok {
				log.Fatalf("You must specifiy a 'query' attribute for input type 'unidata'")
			}

			query, ok := queryField.(string)
			if !ok {
				log.Fatalf("The 'query' attribute for input type 'unidata' must be a string")
			}

			input, err = procs.NewUdtReader(c, query)
			if err != nil {
				log.Fatalf("Error initializing input: %s\n", err)
			}
		}
		processorChain = append(processorChain, input)

		// Initialize Transformations
		for _, transformInfo := range processInfo.Transforms {
			var transform ratchet.DataProcessor
			transformType := strings.ToLower(transformInfo.Type)
			transformConfig := transformInfo.Config

			log.Printf("  Initializing %s transform", transformType)

			switch transformType {
			default:
				log.Fatalf("Unsupported transform type (%s)\n", transformType)
			case "js":
				script := transformConfig["script"].(string)
				transform, err = procs.NewJsTransform(script)
				if err != nil {
					log.Fatalf("Error initializing JS transform: %s", err)
				}
			case "force":
				transform, err = ratchetforce.NewForceTransform(transformConfig["endpoint"].(string))
				if err != nil {
					log.Fatalf("Error initializing Force transform: %s", err)
				}
			}
			processorChain = append(processorChain, transform)
		}

		// Initialize Output
		var output ratchet.DataProcessor
		outputType := strings.ToLower(processInfo.Output.Type)
		outputConfig := processInfo.Output.Config

		log.Printf("  Initializing %s output", outputType)

		switch outputType {
		default:
			log.Fatalf("Unsupported output type (%s)\n", outputType)
		case "csv":
			f := outputFile(outputConfig)
			defer f.Close()
			output = processors.NewCSVWriter(f)
		case "json":
			f := outputFile(outputConfig)
			defer f.Close()
			output = procs.NewJSONWriter(f)
		case "mongodb":
			mgoConfig := &procs.MgoConfig{
				Server:     config.MongoDB.Server,
				Db:         config.MongoDB.Database,
				Collection: outputConfig["collection"].(string),
			}
			output, err = procs.NewMgoWriter(mgoConfig)
			if err != nil {
				log.Fatalf("Error initializing output: %s\n", err)
			}
		}
		processorChain = append(processorChain, output)

		log.Printf("  Initializing data pipeline")
		pipeline := ratchet.NewPipeline(processorChain...)

		log.Printf("  Processesing...")

		err = <-pipeline.Run()
		if err != nil {
			fmt.Println("An error occurred in the data pipeline: ", err.Error())
			os.Exit(1)
		}

		log.Printf(" Done...")

	}

	return nil
}
