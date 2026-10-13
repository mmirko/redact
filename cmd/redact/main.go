package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/mmirko/redact/pkg/embredmon"
	"github.com/redis/go-redis/v9"
)

var (
	debug = flag.Bool("d", false, "Debug")
	test  = flag.Bool("t", false, "Test the system")

	object  = flag.String("object", "", "Object")
	subject = flag.String("subject", "", "Subject")
	action  = flag.String("action", "", "Action")

	check = flag.Bool("c", false, "Check if the action is asserted")
	set   = flag.Bool("s", false, "Assert the action")
	get   = flag.Bool("g", false, "Eventually get the subject")

	scoreTol = flag.Int("scoretol", 60, "Seconds of tolerance on the score")
)

func Debug(logline interface{}) {
	if *debug {
		log.Println("\033[35m[Debug]\033[0m -", logline)
	}
}

func Alert(logline interface{}) {
	log.Println("\033[31m[Alert]\033[0m -", logline)
}

func init() {
	flag.Parse()
}

func redisSource() (embredmon.Sources, error) {
	for _, source := range embredmon.Config.Sources {
		if source.Name == "redmon" && source.Type == "redis" {
			if source.Endpoint == "" {
				return embredmon.Sources{}, errors.New(`Redis source "redmon" has no endpoint`)
			}
			return source, nil
		}
	}
	return embredmon.Sources{}, errors.New(`Redis source "redmon" not found`)
}

func main() {

	source, err := redisSource()
	if err != nil {
		Alert(err)
		os.Exit(2)
	}
	db, err := strconv.Atoi(source.Db)
	if err != nil {
		Alert(fmt.Errorf(`invalid database number in Redis source "redmon": %w`, err))
		os.Exit(2)
	}
	ctx := context.Background()

	// Connecting to Redis, authenticating, and selecting the configured database.
	c := redis.NewClient(&redis.Options{
		Addr:     source.Endpoint,
		Password: source.Key,
		DB:       db,
	})
	defer c.Close()

	if err := c.Ping(ctx).Err(); err != nil {
		Alert(err)
		os.Exit(2)
	}
	Debug("Connection to Redis established")
	Debug("Authentication to Redis succeeded")
	Debug("Selected database " + source.Db)

	// Check the object and the subject

	if *object != "" && *action != "" {
		Debug("Action is \"" + *action + "\", Object is \"" + *object + "\". The redis key is then \"" + *object + ":" + *action + "\"")
	} else {
		Alert("Object and Action cannot be empty")
		os.Exit(2)
	}

	key := *object + ":" + *action

	// Switch operating mode

	switch {
	case *check:

		if *subject != "" {
			// Check for a single subject
			rank, err := c.ZRank(ctx, key, *subject).Result()
			if errors.Is(err, redis.Nil) || (err == nil && rank < 0) {
				Debug("Answer: check negative (missing)")
				os.Exit(1)
			}
			if err != nil {
				Alert("ZRANK failed on key " + key + " with subject " + *subject)
				os.Exit(2)
			}

			subjectTimestamp, err := c.ZScore(ctx, key, *subject).Result()
			if err != nil {
				Alert("Subject timestamp fetching failed")
				os.Exit(2)
			}
			Debug("Subject timestamp " + strconv.FormatFloat(subjectTimestamp, 'f', -1, 64))

			serverTime, err := c.Time(ctx).Result()
			if err != nil {
				Alert("Timestamp fetching failed")
				os.Exit(2)
			}
			timestamp := serverTime.Unix()
			Debug("Timestamp " + strconv.FormatInt(timestamp, 10))

			if float64(timestamp)-subjectTimestamp <= float64(*scoreTol) {
				Debug("Answer: check positive (not expired)")
				if *get {
					fmt.Println(*subject)
				}
				os.Exit(0)
			}
			Debug("Answer: check negative (expired)")
			if err := c.ZRem(ctx, key, *subject).Err(); err == nil {
				Debug("Removed subject " + *subject)
			}
			os.Exit(1)
		} else {
			// Check for any subject.

			// Get the server timestamp
			serverTime, err := c.Time(ctx).Result()
			if err != nil {
				Alert("Timestamp fetching failed")
				os.Exit(2)
			}
			timestamp := serverTime.Unix()
			Debug("Timestamp " + strconv.FormatInt(timestamp, 10))

			scores, err := c.ZRangeWithScores(ctx, key, 0, -1).Result()
			if err != nil {
				Alert("ZRANGE failed on key " + key)
				os.Exit(2)
			}
			if len(scores) == 0 {
				Debug("Answer: check negative (missing)")
				os.Exit(1)
			}

			firstSubject := ""
			checked := false
			for _, score := range scores {
				subjecti := fmt.Sprint(score.Member)
				if float64(timestamp)-score.Score <= float64(*scoreTol) {
					checked = true
					if firstSubject == "" {
						firstSubject = subjecti
					}
				} else if err := c.ZRem(ctx, key, subjecti).Err(); err == nil {
					Debug("Removed subject " + subjecti)
				}
			}
			if checked {
				Debug("Answer: check positive (not expired)")
				if *get {
					fmt.Println(firstSubject)
				}
				os.Exit(0)
			}
			Debug("Answer: check negative (expired)")
			os.Exit(1)
		}

	case *set:

		// Check the subject

		if *subject != "" {
			Debug("Subject is \"" + *subject + "\"")
		} else {
			Alert("Subject cannot be empty in a set operation")
			os.Exit(2)
		}

		// Get the server timestamp

		serverTime, err := c.Time(ctx).Result()
		if err != nil {
			Alert("Timestamp fetching failed")
			os.Exit(2)
		}
		timestamp := serverTime.Unix()
		Debug("Timestamp " + strconv.FormatInt(timestamp, 10))

		// Set the value.
		if err := c.ZAdd(ctx, key, redis.Z{Score: float64(timestamp), Member: *subject}).Err(); err == nil {
			Debug("Set operation succeeded")
		} else {
			Alert("Set operation falied")
			os.Exit(2)
		}

	default:
		Alert("No operating mode specified choose among check (-c) or set (-s)")
		os.Exit(2)
	}

	os.Exit(0)

}
