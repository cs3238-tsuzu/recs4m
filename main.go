package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/boltdb/bolt"
	"github.com/gorilla/mux"
)

type Reservation struct {
	ID        string
	Title     string
	DayOfWeek int
	StartTime int
	Duration  int
	Everyweek bool
}

type Log struct {
	Time    time.Time
	Message string
}

func NextStartTime(resv Reservation) time.Time {
	now := time.Now()
	t := now
	_, offset := t.Zone()
	t = t.
		Add(time.Duration(offset) * time.Second).
		Truncate(24 * time.Hour).
		Add(-time.Duration(offset) * time.Second).
		Add(time.Duration(resv.StartTime) * time.Minute)

	addition := t.Weekday() - time.Weekday(resv.DayOfWeek)
	if addition < 0 {
		addition += 7
	}

	t = t.AddDate(0, 0, int(addition))

	for t.Sub(now) <= 0 {
		t = t.AddDate(0, 0, 7)
	}

	return t
}

// Bucket name for boltdb
var SettingBucket = []byte("setting")
var ReservationsBucket = []byte("reservations")
var RecentLogsBucket = []byte("recent")

func timeFromString(str string) (int, bool) {
	arr := strings.Split(str, ":")

	if len(arr) != 2 {
		return 0, false
	}

	var h, m int
	var err error
	if h, err = strconv.Atoi(arr[0]); err != nil {
		return 0, false
	}
	if m, err = strconv.Atoi(arr[1]); err != nil {
		return 0, false
	}

	return h*60 + m, true
}

func main() {
	const Location = "Asia/Tokyo"

	loc, err := time.LoadLocation(Location)
	if err != nil {
		loc = time.FixedZone(Location, 9*60*60)
	}

	time.Local = loc

	streamURL := flag.String("stream", "http://tsuzu2.cloudapp.net/audio", "MP3 stream path")
	uploadScript := flag.String("upload-script", "./upload.sh", "path to script for uploading")
	debug := flag.Bool("debug", false, "Debug output")

	if *debug {
		logrus.SetLevel(logrus.DebugLevel)
	}

	tmpl, err := template.New("template").
		Funcs(template.FuncMap{
			"dayOfWeekToString": func(index int) string {
				return (time.Sunday + time.Weekday(index)).String()
			},
			"timeToString": func(t int) string {
				return fmt.Sprintf("%02d:%02d", t/60, t%60)
			},
			"addDuration": func(t, d int) int {
				return t + d
			},
			"dtToString": func(t time.Time) string {
				return t.Format("Mon, 02 Jan 2006 15:04:05")
			},
		}).
		ParseFiles("./html/index.html", "./html/new.html")

	if err != nil {
		panic(err)
	}

	for _, v := range tmpl.Templates() {
		fmt.Println(v.Name())
	}

	dbPath := os.Getenv("BOLTDB")
	if len(dbPath) == 0 {
		dbPath = "recs4m.db"
	}

	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to open boltdb")
	}
	defer db.Close()

	if err := db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(ReservationsBucket); err != nil {
			return err
		}

		if _, err := tx.CreateBucketIfNotExists(RecentLogsBucket); err != nil {
			return err
		}

		if _, err := tx.CreateBucketIfNotExists(SettingBucket); err != nil {
			return err
		}

		return nil
	}); err != nil {
		logrus.WithError(err).Error("Bucket creating failed")
	}

	addNewLog := func(message string) {
		lg := Log{
			Time:    time.Now(),
			Message: message,
		}

		if err := db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket(RecentLogsBucket)

			encoded, _ := json.Marshal(lg)
			return b.Put([]byte(time.Now().Format(time.RFC3339)), encoded)
		}); err != nil {
			logrus.WithError(err).Error("Logging into Bolt error")
		}
	}

	addNewLog("Launched")

	router := mux.NewRouter()

	router.HandleFunc("/", func(rw http.ResponseWriter, req *http.Request) {
		tmpl := tmpl.Lookup("index.html")

		type ReservationWrapped struct {
			Reservation
			Index int
		}

		type TemplateData struct {
			Reservations []ReservationWrapped
			Logs         []Log
		}
		var tdata TemplateData

		err := db.View(func(tx *bolt.Tx) error {
			idx := 0
			if err := tx.Bucket(ReservationsBucket).ForEach(func(_, v []byte) error {
				var resv Reservation
				if err := json.Unmarshal(v, &resv); err != nil {
					return err
				}

				idx++
				tdata.Reservations = append(tdata.Reservations, ReservationWrapped{resv, idx})

				return nil
			}); err != nil {
				return err
			}

			b := tx.Bucket(RecentLogsBucket)

			if b == nil {
				return bolt.ErrBucketNotFound
			}
			c := b.Cursor()

			for k, v := c.Last(); k != nil; k, v = c.Prev() {
				var lg Log
				if err := json.Unmarshal(v, &lg); err != nil {
					return err
				}

				tdata.Logs = append(tdata.Logs, lg)
			}

			return nil
		})

		if err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			rw.Write([]byte("International Server Error"))

			logrus.WithError(err).Error("/ error: error occured while BoltDB transaction")

			return
		}

		tmpl.Execute(rw, tdata)
	})

	router.HandleFunc("/new", func(rw http.ResponseWriter, req *http.Request) {
		if req.Method == "GET" {
			tmpl.Lookup("new.html").Execute(rw, nil)
		} else if req.Method == "POST" {
			if err := req.ParseForm(); err != nil {
				rw.WriteHeader(http.StatusBadRequest)

				return
			}

			newResv := Reservation{
				Title:     req.FormValue("title"),
				Everyweek: req.FormValue("everyweek") == "checked",
			}

			var ok bool
			if newResv.StartTime, ok = timeFromString(req.FormValue("startTime")); !ok {
				rw.WriteHeader(http.StatusBadRequest)

				return
			}

			var err error
			newResv.DayOfWeek, err = strconv.Atoi(req.FormValue("dayOfWeek"))

			if err != nil {
				rw.WriteHeader(http.StatusBadRequest)

				return
			}

			newResv.Duration, err = strconv.Atoi(req.FormValue("duration"))

			if err != nil {
				rw.WriteHeader(http.StatusBadRequest)

				return
			}

			newResv.ID = time.Now().Format(time.RFC3339) + fmt.Sprintf("%03d", rand.Intn(1000))

			encoded, _ := json.Marshal(newResv)
			if err := db.Update(func(tx *bolt.Tx) error {
				b := tx.Bucket(ReservationsBucket)

				return b.Put([]byte(newResv.ID), encoded)
			}); err != nil {
				rw.WriteHeader(http.StatusInternalServerError)

				logrus.WithError(err).Error("/update error")
			}

			rw.Header().Set("Location", "/")
			rw.WriteHeader(http.StatusFound)
		} else {
			rw.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	router.HandleFunc("/remove/{id}", func(rw http.ResponseWriter, req *http.Request) {
		id := mux.Vars(req)["id"]

		if err := db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket(ReservationsBucket)

			return b.Delete([]byte(id))
		}); err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			rw.Write([]byte("Internal Server Error"))

			logrus.WithError(err).Error("/remove/{id} error")
		}

		rw.Header().Set("Location", "/")
		rw.WriteHeader(http.StatusFound)
	})

	router.HandleFunc("/update", func(rw http.ResponseWriter, req *http.Request) {
		if req.Method != "POST" {
			rw.WriteHeader(http.StatusMethodNotAllowed)

			return
		}

		if err := req.ParseForm(); err != nil {
			rw.WriteHeader(http.StatusBadRequest)

			return
		}

		newResv := Reservation{
			ID:        req.FormValue("id"),
			Title:     req.FormValue("title"),
			Everyweek: req.FormValue("everyweek") == "checked",
		}

		var ok bool
		if newResv.StartTime, ok = timeFromString(req.FormValue("startTime")); !ok {
			rw.WriteHeader(http.StatusBadRequest)

			return
		}

		var err error
		newResv.DayOfWeek, err = strconv.Atoi(req.FormValue("dayOfWeek"))

		if err != nil {
			rw.WriteHeader(http.StatusBadRequest)

			return
		}

		newResv.Duration, err = strconv.Atoi(req.FormValue("duration"))

		if err != nil {
			rw.WriteHeader(http.StatusBadRequest)

			return
		}

		newResv.ID = time.Now().Format(time.RFC3339) + fmt.Sprintf("%03d", rand.Intn(1000))

		encoded, _ := json.Marshal(newResv)

		if err := db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket(ReservationsBucket)

			return b.Put([]byte(newResv.ID), encoded)
		}); err != nil {
			rw.WriteHeader(http.StatusInternalServerError)

			logrus.WithError(err).Error("/update error")
		}

		rw.Header().Set("Location", "/")
		rw.WriteHeader(http.StatusFound)
	})

	listen := os.Getenv("LISTEN")
	if len(listen) == 0 {
		listen = ":80"
	}

	go func() {
		recording := sync.Map{}

		asyncRecord := func(startTime time.Time, resv Reservation) {
			var retErr error
			defer func() {
				if retErr != nil {
					logrus.WithError(retErr).Error("Recording error")

					addNewLog(fmt.Sprintf("Failed: %s(from: %s, reason: %s)", resv.Title, startTime.String(), retErr.Error()))
				}
			}()
			defer recording.Delete(resv.ID)
			time.Sleep(time.Now().Add(30 * time.Second).Sub(startTime))

			addNewLog(fmt.Sprintf("Recording started: %s(from: %s)", resv.Title, startTime))

			resp, err := http.Get(*streamURL)
			if err != nil {
				retErr = err

				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != 200 {
				retErr = errors.New(fmt.Sprint("Status Code=", resp.StatusCode))

				return
			}

			fp, err := ioutil.TempFile("/tmp", "recs4m")

			if err != nil {
				retErr = err

				return
			}
			buf := make([]byte, 1024)
			for {
				n, err := resp.Body.Read(buf)

				if err != nil {
					retErr = err

					return
				}

				if time.Now().Sub(startTime.Add(10*time.Second)) < 0 {
					continue
				}

				if time.Now().Sub(startTime.Add(time.Duration(resv.Duration))) >= 0 {
					break
				}

				if _, err := fp.Write(buf[:n]); err != nil {
					retErr = err

					return
				}
			}

			name := fp.Name()
			fp.Close()

			if err := os.Rename("/tmp"+name, "/tmp"+name+".mp3"); err != nil {
				retErr = err

				return
			}
			resp.Body.Close()

			_, err = exec.Command("sh", "-c", *uploadScript, "/tmp"+name+".mp3", resv.Title, startTime.String()).CombinedOutput()

			if err != nil {
				retErr = err

				return
			}

			addNewLog(fmt.Sprintf("Successfully recorded and uploaded: %s(from: %s)", resv.Title, startTime.String()))
		}

		ticker := time.NewTicker(1 * time.Minute)

		for {
			<-ticker.C
			logrus.WithField("time", time.Now().String()).Debug("Ticker event")

			deletedOnce := make([]string, 0, 10)
			db.View(func(tx *bolt.Tx) error {
				b := tx.Bucket(ReservationsBucket)

				return b.ForEach(func(_, v []byte) error {
					var resv Reservation
					if err := json.Unmarshal(v, &resv); err != nil {
						return err
					}

					st := NextStartTime(resv)

					logrus.WithField("startTime", st).Debug("Checking")
					if st.Sub(time.Now()) < 2*time.Minute {
						if _, ok := recording.Load(resv.ID); !ok {
							recording.Store(resv.ID, true)

							if !resv.Everyweek {
								deletedOnce = append(deletedOnce, resv.ID)
							}

							go asyncRecord(st, resv)
						}
					}
					return nil
				})
			})

			db.Update(func(tx *bolt.Tx) error {
				b := tx.Bucket(ReservationsBucket)

				for i := range deletedOnce {
					if err := b.Delete([]byte(deletedOnce[i])); err != nil {
						logrus.WithError(err).Error("Deletion error")
					}
				}

				return nil
			})
		}
	}()

	panic(http.ListenAndServe(listen, router))
}
