/*
  Copyright (c) 2022-, Germano Rizzo <oss /AT/ germanorizzo /DOT/ it>

  Permission to use, copy, modify, and/or distribute this software for any
  purpose with or without fee is hereby granted, provided that the above
  copyright notice and this permission notice appear in all copies.

  THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
  WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
  MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
  ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
  WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
  ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
  OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
*/

package main

import (
	"encoding/json"
	"github.com/gofiber/fiber/v2"
	"os"
	"sync"
	"testing"
	"time"

	mllog "github.com/proofrock/go-mylittlelogger"
)

const concurrency = 64

func TestMain(m *testing.M) {
	println("Go...")
	oldLevel := mllog.Level
	mllog.Level = mllog.NOT_EVEN_STDERR
	exitCode := m.Run()
	mllog.Level = oldLevel
	println("...finished")
	os.Exit(exitCode)
}

func Shutdown() {
	stopScheduler()
	if len(dbs) > 0 {
		mllog.StdOut("Closing databases...")
		for i := range dbs {
			if dbs[i].DbConn != nil {
				dbs[i].DbConn.Close()
			}
			dbs[i].Db.Close()
			delete(dbs, i)
		}
	}
	if app != nil {
		mllog.StdOut("Shutting down web server...")
		app.Shutdown()
		app = nil
	}
}

// call with basic auth support
func callBA(databaseId string, req request, user, password string, t *testing.T) (int, string, response) {
	json_data, err := json.Marshal(req)
	if err != nil {
		t.Error(err)
	}

	client := &fiber.Client{}
	post := client.Post("http://localhost:12321/"+databaseId).
		Body(json_data).
		Set("Content-Type", "application/json")

	if user != "" {
		post = post.BasicAuth(user, password)
	}

	code, bodyBytes, errs := post.Bytes()

	if errs != nil && len(errs) > 0 {
		t.Error(errs[0])
	}

	var res response
	if err := json.Unmarshal(bodyBytes, &res); code == 200 && err != nil {
		println(string(bodyBytes))
		t.Error(err)
	}
	return code, string(bodyBytes), res
}

func call(databaseId string, req request, t *testing.T) (int, string, response) {
	return callBA(databaseId, req, "", "", t)
}

func mkRaw(mapp map[string]interface{}) map[string]json.RawMessage {
	ret := make(map[string]json.RawMessage)
	for k, v := range mapp {
		bytes, _ := json.Marshal(v)
		ret[k] = bytes
	}
	return ret
}

func TestSetup(t *testing.T) {
	os.Remove("../test/test.db")

	cfg := config{
		Bindhost: "0.0.0.0",
		Port:     12321,
		Databases: []db{
			{
				Id:   "test",
				Path: "../test/test.db",
				//DisableWALMode: true,
				StoredStatement: []storedStatement{
					{
						Id:  "Q",
						Sql: "SELECT 1",
					},
				},
			},
		},
	}
	go launch(cfg, true)

	time.Sleep(time.Second)

	if !fileExists("../test/test.db") {
		t.Error("db file not created")
		return
	}
}

func TestCreate(t *testing.T) {
	req := request{
		Transaction: []requestItem{
			{
				Statement: "CREATE TABLE T1 (ID INT PRIMARY KEY, VAL TEXT NOT NULL)",
			},
		},
	}

	code, _, res := call("test", req, t)

	if code != 200 {
		t.Error("did not succeed")
		return
	}

	if !res.Results[0].Success {
		t.Error("did not succeed")
	}
}

func TestFail(t *testing.T) {
	req := request{
		Transaction: []requestItem{
			{
				Statement: "CREATE TABLE T1 (ID INT PRIMARY KEY, VAL TEXT NOT NULL)",
			},
		},
	}

	code, _, _ := call("test", req, t)

	if code != 500 {
		t.Error("did succeed, but shouldn't")
	}
}

func TestTx(t *testing.T) {
	req := request{
		Transaction: []requestItem{
			{
				Statement: "INSERT INTO T1 (ID, VAL) VALUES (1, 'ONE')",
			},
			{
				Statement: "INSERT INTO T1 (ID, VAL) VALUES (1, 'TWO')",
				NoFail:    true,
			},
			{
				Query: "SELECT * FROM T1 WHERE ID = 1",
			},
			{
				Statement: "INSERT INTO T1 (ID, VAL) VALUES (:ID, :VAL)",
				Values: mkRaw(map[string]interface{}{
					"ID":  2,
					"VAL": "TWO",
				}),
			},
			{
				Statement: "INSERT INTO T1 (ID, VAL) VALUES (:ID, :VAL)",
				ValuesBatch: []map[string]json.RawMessage{
					mkRaw(map[string]interface{}{
						"ID":  3,
						"VAL": "THREE",
					}),
					mkRaw(map[string]interface{}{
						"ID":  4,
						"VAL": "FOUR",
					})},
			},
			{
				Query: "SELECT * FROM T1 WHERE ID > :ID",
				Values: mkRaw(map[string]interface{}{
					"ID": 0,
				}),
			},
		},
	}

	code, _, res := call("test", req, t)

	if code != 200 {
		t.Error("did not succeed")
		return
	}

	if !res.Results[0].Success || *res.Results[0].RowsUpdated != 1 {
		t.Error("req 0 inconsistent")
	}

	if res.Results[1].Success {
		t.Error("req 1 inconsistent")
	}

	if !res.Results[2].Success || res.Results[2].ResultSet[0]["VAL"] != "ONE" {
		t.Error("req 2 inconsistent")
	}

	if !res.Results[3].Success || *res.Results[3].RowsUpdated != 1 {
		t.Error("req 3 inconsistent")
	}

	if !res.Results[4].Success || len(res.Results[4].RowsUpdatedBatch) != 2 {
		t.Error("req 4 inconsistent")
	}

	if !res.Results[5].Success || len(res.Results[5].ResultSet) != 4 {
		t.Error("req 5 inconsistent")
	}

}

func TestTxRollback(t *testing.T) {
	req := request{
		Transaction: []requestItem{
			{
				Statement: "DELETE FROM T1",
			},
			{
				Statement: "INSERT INTO T1 (ID, VAL) VALUES (1, 'ONE')",
			},
			{
				Statement: "INSERT INTO T1 (ID, VAL) VALUES (1, 'ONE')",
			},
		},
	}

	code, _, res := call("test", req, t)

	if code != 500 {
		t.Error("did succeed, but should have not")
		return
	}

	req = request{
		Transaction: []requestItem{
			{
				Query: "SELECT * FROM T1",
			},
		},
	}

	code, _, res = call("test", req, t)

	if code != 200 {
		t.Error("did not succeed")
		return
	}

	if !res.Results[0].Success || len(res.Results[0].ResultSet) != 4 {
		t.Error("req 0 inconsistent")
	}
}

func TestSQ(t *testing.T) {
	req := request{
		Transaction: []requestItem{
			{
				Query: "#Q",
			},
		},
	}

	code, _, res := call("test", req, t)

	if code != 200 {
		t.Error("did not succeed")
		return
	}

	if !res.Results[0].Success {
		t.Error("did not succeed")
	}
}

func TestConcurrent(t *testing.T) {
	req := request{
		Transaction: []requestItem{
			{
				Statement: "DELETE FROM T1; INSERT INTO T1 (ID, VAL) VALUES (1, 'ONE')",
			},
			{
				Statement: "INSERT INTO T1 (ID, VAL) VALUES (1, 'TWO')",
				NoFail:    true,
			},
			{
				Query: "SELECT * FROM T1 WHERE ID = 1",
			},
			{
				Statement: "INSERT INTO T1 (ID, VAL) VALUES (:ID, :VAL)",
				Values: mkRaw(map[string]interface{}{
					"ID":  2,
					"VAL": "TWO",
				}),
			},
			{
				Statement: "INSERT INTO T1 (ID, VAL) VALUES (:ID, :VAL)",
				ValuesBatch: []map[string]json.RawMessage{
					mkRaw(map[string]interface{}{
						"ID":  3,
						"VAL": "THREE",
					}),
					mkRaw(map[string]interface{}{
						"ID":  4,
						"VAL": "FOUR",
					})},
			},
			{
				Query: "SELECT * FROM T1 WHERE ID > :ID",
				Values: mkRaw(map[string]interface{}{
					"ID": 0,
				}),
			},
		},
	}

	wg := new(sync.WaitGroup)
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func(t *testing.T) {
			defer wg.Done()
			code, body, res := call("test", req, t)

			if code != 200 {
				t.Errorf("did not succeed, code was %d - %s", code, body)
				return
			}

			if !res.Results[0].Success || *res.Results[0].RowsUpdated != 1 {
				t.Error("req 0 inconsistent")
			}

			if res.Results[1].Success {
				t.Error("req 1 inconsistent")
			}

			if !res.Results[2].Success || res.Results[2].ResultSet[0]["VAL"] != "ONE" {
				t.Error("req 2 inconsistent")
			}

			if !res.Results[3].Success || *res.Results[3].RowsUpdated != 1 {
				t.Error("req 3 inconsistent")
			}

			if !res.Results[4].Success || len(res.Results[4].RowsUpdatedBatch) != 2 {
				t.Error("req 4 inconsistent")
			}

			if !res.Results[5].Success || len(res.Results[5].ResultSet) != 4 {
				t.Error("req 5 inconsistent")
			}
		}(t)
	}
	wg.Wait()
}

// don't remove the file, we'll use it for the next tests for read-only
func TestTeardown(t *testing.T) {
	time.Sleep(time.Second)
	Shutdown()
}

// Tests for read-only connections

func TestSetupRO(t *testing.T) {
	cfg := config{
		Bindhost: "0.0.0.0",
		Port:     12321,
		Databases: []db{
			{
				Id:   "test",
				Path: "../test/test.db",
				//DisableWALMode: true,
				ReadOnly: true,
				StoredStatement: []storedStatement{
					{
						Id:  "Q",
						Sql: "SELECT 1",
					},
				},
			},
		},
	}
	go launch(cfg, true)

	time.Sleep(time.Second)
}

func TestFailRO(t *testing.T) {
	req := request{
		Transaction: []requestItem{
			{
				Statement: "CREATE TABLE T1 (ID INT PRIMARY KEY, VAL TEXT NOT NULL)",
			},
		},
	}

	code, _, _ := call("test", req, t)

	if code != 500 {
		t.Error("did succeed, but shouldn't")
	}
}

func TestOkRO(t *testing.T) {
	req := request{
		Transaction: []requestItem{
			{
				Query: "SELECT * FROM T1 ORDER BY ID ASC",
			},
		},
	}

	code, body, res := call("test", req, t)

	if code != 200 {
		t.Errorf("did not succeed, but should have: %s", body)
		return
	}

	if !res.Results[0].Success || res.Results[0].ResultSet[3]["VAL"] != "FOUR" {
		t.Error("req is inconsistent")
	}
}

func TestConcurrentRO(t *testing.T) {
	req := request{
		Transaction: []requestItem{
			{
				Query: "SELECT * FROM T1 ORDER BY ID ASC",
			},
		},
	}

	wg := new(sync.WaitGroup)
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func(t *testing.T) {
			defer wg.Done()
			code, body, res := call("test", req, t)

			if code != 200 {
				t.Errorf("did not succeed, code was %d - %s", code, body)
				return
			}

			if !res.Results[0].Success || res.Results[0].ResultSet[3]["VAL"] != "FOUR" {
				t.Error("req is inconsistent")
			}
		}(t)
	}
	wg.Wait()
}

func TestTeardownRO(t *testing.T) {
	time.Sleep(time.Second)
	Shutdown()
}

// Tests for stored-statements-only connections

func TestSetupSQO(t *testing.T) {
	cfg := config{
		Bindhost: "0.0.0.0",
		Port:     12321,
		Databases: []db{
			{
				Id:   "test",
				Path: "../test/test.db",
				//DisableWALMode: true,
				ReadOnly:                true,
				UseOnlyStoredStatements: true,
				StoredStatement: []storedStatement{
					{
						Id:  "Q",
						Sql: "SELECT 1",
					},
				},
			},
		},
	}
	go launch(cfg, true)

	time.Sleep(time.Second)
}

func TestFailSQO(t *testing.T) {
	req := request{
		Transaction: []requestItem{
			{
				Statement: "SELECT 1",
			},
		},
	}

	code, _, _ := call("test", req, t)

	if code != 400 {
		t.Error("did succeed, but shouldn't")
	}
}

func TestOkSQO(t *testing.T) {
	req := request{
		Transaction: []requestItem{
			{
				Query: "#Q",
			},
		},
	}

	code, body, res := call("test", req, t)

	if code != 200 {
		t.Errorf("did not succeed, but should have: %s", body)
		return
	}

	if !res.Results[0].Success {
		t.Error("req is inconsistent")
	}
}

func TestTeardownSQO(t *testing.T) {
	time.Sleep(time.Second)
	Shutdown()
	os.Remove("../test/test.db")
}

func TestSetupMEM(t *testing.T) {
	cfg := config{
		Bindhost: "0.0.0.0",
		Port:     12321,
		Databases: []db{
			{
				Id:   "test",
				Path: ":memory:",
				//DisableWALMode: true,
				StoredStatement: []storedStatement{
					{
						Id:  "Q",
						Sql: "SELECT 1",
					},
				},
			},
		},
	}
	go launch(cfg, true)

	time.Sleep(time.Second)
}

func TestMEM(t *testing.T) {
	req := request{
		Transaction: []requestItem{
			{
				Statement: "CREATE TABLE T1 (ID INT PRIMARY KEY, VAL TEXT NOT NULL)",
			},
		},
	}

	code, _, res := call("test", req, t)

	if code != 200 {
		t.Error("did not succeed")
		return
	}

	if !res.Results[0].Success {
		t.Error("did not succeed")
	}
}

func TestMEMIns(t *testing.T) {
	req := request{
		Transaction: []requestItem{
			{
				Statement: "INSERT INTO T1 (ID, VAL) VALUES (1, 'ONE')",
			},
		},
	}

	code, _, res := call("test", req, t)

	if code != 200 {
		t.Error("did not succeed")
		return
	}

	if !res.Results[0].Success {
		t.Error("did not succeed")
	}
}

func TestTeardownMEM(t *testing.T) {
	time.Sleep(time.Second)
	Shutdown()
}

func TestSetupMEM_RO(t *testing.T) {
	cfg := config{
		Bindhost: "0.0.0.0",
		Port:     12321,
		Databases: []db{
			{
				Id:       "test",
				Path:     ":memory:",
				ReadOnly: true,
				//DisableWALMode: true,
				StoredStatement: []storedStatement{
					{
						Id:  "Q",
						Sql: "SELECT 1",
					},
				},
			},
		},
	}
	go launch(cfg, true)

	time.Sleep(time.Second)
}

func TestMEM_RO(t *testing.T) {
	req := request{
		Transaction: []requestItem{
			{
				Query: "SELECT 1",
			},
		},
	}

	code, _, res := call("test", req, t)

	if code != 200 {
		t.Error("did not succeed")
		return
	}

	if !res.Results[0].Success {
		t.Error("did not succeed")
	}
}

func TestTeardownMEM_RO(t *testing.T) {
	time.Sleep(time.Second)
	Shutdown()
}

func TestSetupWITH_ADD_PROPS(t *testing.T) {
	cfg := config{
		Bindhost: "0.0.0.0",
		Port:     12321,
		Databases: []db{
			{
				Id:   "test",
				Path: "file::memory:",
				//DisableWALMode: true,
				StoredStatement: []storedStatement{
					{
						Id:  "Q",
						Sql: "SELECT 1",
					},
				},
			},
		},
	}
	go launch(cfg, true)

	time.Sleep(time.Second)
}

func TestWITH_ADD_PROPS(t *testing.T) {
	req := request{
		Transaction: []requestItem{
			{
				Query: "CREATE TABLE T1 (ID INT PRIMARY KEY, VAL TEXT NOT NULL)",
			},
		},
	}

	code, _, res := call("test", req, t)

	if code != 200 {
		t.Error("did not succeed")
		return
	}

	if !res.Results[0].Success {
		t.Error("did not succeed")
	}
}

func TestTeardownWITH_ADD_PROPS(t *testing.T) {
	time.Sleep(time.Second)
	Shutdown()
}

func TestRO_MEM_IS(t *testing.T) {
	// checks if it's possible to create a read only db with init statements (it shouldn't)
	cfg := config{
		Bindhost: "0.0.0.0",
		Port:     12321,
		Databases: []db{
			{
				Id:       "test",
				Path:     ":memory:",
				ReadOnly: true,
				//DisableWALMode: true,
				InitStatements: []string{
					"CREATE TABLE T1 (ID INT)",
				},
			},
		},
	}
	success := true
	mllog.WhenFatal = func(msg string) { success = false }
	defer func() { mllog.WhenFatal = func(msg string) { os.Exit(1) } }()
	go launch(cfg, true)
	time.Sleep(time.Second)
	Shutdown()
	if success {
		t.Error("did succeed, but shouldn't have")
	}
}

func Test_IS_Err(t *testing.T) {
	// checks if it exists after a failed init statement
	cfg := config{
		Bindhost: "0.0.0.0",
		Port:     12321,
		Databases: []db{
			{
				Id:   "test",
				Path: ":memory:",
				//DisableWALMode: true,
				InitStatements: []string{
					"CREATE TABLE T1 (ID INT)",
					"CREATE TABLE T1 (ID INT)",
				},
			},
		},
	}
	success := true
	mllog.WhenFatal = func(msg string) { success = false }
	defer func() { mllog.WhenFatal = func(msg string) { os.Exit(1) } }()
	go launch(cfg, true)
	time.Sleep(time.Second)
	Shutdown()
	if success {
		t.Error("did succeed, but shouldn't have")
	}
}

func Test_DoubleId_Err(t *testing.T) {
	cfg := config{
		Bindhost: "0.0.0.0",
		Port:     12321,
		Databases: []db{
			{
				Id:   "test",
				Path: ":memory:",
			}, {
				Id:   "test",
				Path: ":memory:",
			},
		},
	}
	success := true
	mllog.WhenFatal = func(msg string) { success = false }
	defer func() { mllog.WhenFatal = func(msg string) { os.Exit(1) } }()
	go launch(cfg, true)
	time.Sleep(time.Second)
	Shutdown()
	if success {
		t.Error("did succeed, but shouldn't have")
	}
}

func Test_DelWhenInitFails(t *testing.T) {
	defer Shutdown()
	defer os.Remove("../test/test.db")
	defer os.Remove("../test/test.db-shm")
	defer os.Remove("../test/test.db-wal")
	os.Remove("../test/test.db")
	os.Remove("../test/test.db-shm")
	os.Remove("../test/test.db-wal")

	mllog.WhenFatal = func(msg string) {}
	defer func() { mllog.WhenFatal = func(msg string) { os.Exit(1) } }()

	cfg := config{
		Bindhost: "0.0.0.0",
		Port:     12321,
		Databases: []db{
			{
				Id:   "test",
				Path: "../test/test.db",
				InitStatements: []string{
					"CLEARLY INVALID SQL",
				},
			},
		},
	}
	go launch(cfg, true)
	time.Sleep(time.Second)

	if fileExists("../test/test.db") {
		t.Error("file wasn't cleared")
	}
}

// If I put a question mark in the path, it must not interfere with the
// ability to check if it's a new file. The second creation below
// should NOT fail, as it's not a new file.
func Test_CreateWithQuestionMark(t *testing.T) {
	defer Shutdown()
	defer os.Remove("../test/test.db")
	defer os.Remove("../test/test.db-shm")
	defer os.Remove("../test/test.db-wal")
	os.Remove("../test/test.db")
	os.Remove("../test/test.db-shm")
	os.Remove("../test/test.db-wal")

	success := true

	mllog.WhenFatal = func(msg string) { success = false }
	defer func() { mllog.WhenFatal = func(msg string) { os.Exit(1) } }()

	cfg := config{
		Bindhost: "0.0.0.0",
		Port:     12321,
		Databases: []db{
			{
				Id:   "test",
				Path: "../test/test.db",
				InitStatements: []string{
					"CREATE TABLE T1 (ID INT)",
				},
			},
		},
	}

	go launch(cfg, true)
	time.Sleep(time.Second)
	Shutdown()

	if !success {
		t.Error("did not succeed, but should have")
	}

	cfg = config{
		Bindhost: "0.0.0.0",
		Port:     12321,
		Databases: []db{
			{
				Id:   "test",
				Path: "../test/test.db",
				InitStatements: []string{
					"CREATE TABLE T1 (ID INT)",
				},
			},
		},
	}
	go launch(cfg, true)
	time.Sleep(time.Second)

	if !success {
		t.Error("did not succeed, but should have")
	}
}

func TestTwoServesOneDb(t *testing.T) {
	defer Shutdown()
	defer os.Remove("../test/test.db")
	defer os.Remove("../test/test.db-shm")
	defer os.Remove("../test/test.db-wal")
	os.Remove("../test/test.db")
	os.Remove("../test/test.db-shm")
	os.Remove("../test/test.db-wal")

	cfg := config{
		Bindhost: "0.0.0.0",
		Port:     12321,
		Databases: []db{
			{
				Id:   "test1",
				Path: "../test/test.db",
				InitStatements: []string{
					"CREATE TABLE T (NUM INT)",
				},
			}, {
				Id:       "test2",
				Path:     "../test/test.db",
				ReadOnly: true,
			},
		},
	}

	go launch(cfg, true)

	time.Sleep(time.Second)

	req1 := request{
		Transaction: []requestItem{
			{
				Statement: "INSERT INTO T VALUES (25)",
			},
		},
	}
	req2 := request{
		Transaction: []requestItem{
			{
				Query: "SELECT COUNT(1) FROM T",
			},
		},
	}

	wg := new(sync.WaitGroup)
	wg.Add(concurrency * 2)

	for i := 0; i < concurrency; i++ {
		go func(t *testing.T) {
			defer wg.Done()
			code, body, _ := call("test1", req1, t)
			if code != 200 {
				t.Error("INSERT failed", body)
			}
		}(t)
		go func(t *testing.T) {
			defer wg.Done()
			code, body, _ := call("test2", req2, t)
			if code != 200 {
				t.Error("SELECT failed", body)
			}
		}(t)
	}

	wg.Wait()

	time.Sleep(time.Second)
}

// Test about the various field of a ResponseItem being null
// when not actually involved

func TestItemFieldsSetup(t *testing.T) {
	os.Remove("../test/test.db")

	cfg := config{
		Bindhost: "0.0.0.0",
		Port:     12321,
		Databases: []db{
			{
				Id:   "test",
				Path: ":memory:",
				InitStatements: []string{
					"CREATE TABLE T1 (ID INT PRIMARY KEY, VAL TEXT NOT NULL)",
				},
			},
		},
	}
	go launch(cfg, true)

	time.Sleep(time.Second)
}

func TestItemFieldsEmptySelect(t *testing.T) {
	req := request{
		Transaction: []requestItem{
			{
				Query: "SELECT 1 WHERE 0 = 1",
			},
		},
	}

	code, _, res := call("test", req, t)

	if code != 200 {
		t.Error("did not succeed")
		return
	}

	if !res.Results[0].Success {
		t.Error("did not succeed")
	}

	resItem := res.Results[0]

	if resItem.ResultSet == nil {
		t.Error("select result is nil")
	}

	if resItem.Error != "" {
		t.Error("error is not empty")
	}

	if resItem.RowsUpdated != nil {
		t.Error("rowsUpdated is not nil")
	}

	if resItem.RowsUpdatedBatch != nil {
		t.Error("rowsUpdatedBatch is not nil")
	}
}

func TestItemFieldsInsert(t *testing.T) {
	req := request{
		Transaction: []requestItem{
			{
				Statement: "INSERT INTO T1 VALUES (1, 'a')",
			},
		},
	}

	code, _, res := call("test", req, t)

	if code != 200 {
		t.Error("did not succeed")
		return
	}

	if !res.Results[0].Success {
		t.Error("did not succeed")
	}

	resItem := res.Results[0]

	if resItem.ResultSet != nil {
		t.Error("select result is not nil")
	}

	if resItem.Error != "" {
		t.Error("error is not empty")
	}

	if resItem.RowsUpdated == nil {
		t.Error("rowsUpdated is nil")
	}

	if resItem.RowsUpdatedBatch != nil {
		t.Error("rowsUpdatedBatch is not nil")
	}
}

func TestItemFieldsInsertBatch(t *testing.T) {
	req := request{
		Transaction: []requestItem{
			{
				Statement: "INSERT INTO T1 VALUES (:ID, :VAL)",
				ValuesBatch: []map[string]json.RawMessage{
					mkRaw(map[string]interface{}{
						"ID":  3,
						"VAL": "THREE",
					}),
					mkRaw(map[string]interface{}{
						"ID":  4,
						"VAL": "FOUR",
					})},
			},
		},
	}

	code, _, res := call("test", req, t)

	if code != 200 {
		t.Error("did not succeed")
		return
	}

	if !res.Results[0].Success {
		t.Error("did not succeed")
	}

	resItem := res.Results[0]

	if resItem.ResultSet != nil {
		t.Error("select result is not nil")
	}

	if resItem.Error != "" {
		t.Error("error is not empty")
	}

	if resItem.RowsUpdated != nil {
		t.Error("rowsUpdated is not nil")
	}

	if resItem.RowsUpdatedBatch == nil {
		t.Error("rowsUpdatedBatch is nil")
	}
}

func TestItemFieldsError(t *testing.T) {
	req := request{
		Transaction: []requestItem{
			{
				Query:  "A CLEARLY INVALID SQL",
				NoFail: true,
			},
		},
	}

	code, _, res := call("test", req, t)

	if code != 200 {
		t.Error("did not succeed")
		return
	}

	if res.Results[0].Success {
		t.Error("did succeed, but it shoudln't have")
	}

	resItem := res.Results[0]

	if resItem.ResultSet != nil {
		t.Error("select result is not nil")
	}

	if resItem.Error == "" {
		t.Error("error is empty")
	}

	if resItem.RowsUpdated != nil {
		t.Error("rowsUpdated is not nil")
	}

	if resItem.RowsUpdatedBatch != nil {
		t.Error("rowsUpdatedBatch is not nil")
	}
}

func TestItemFieldsTeardown(t *testing.T) {
	time.Sleep(time.Second)
	Shutdown()
}

func TestUnicode(t *testing.T) {
	cfg := config{
		Bindhost: "0.0.0.0",
		Port:     12321,
		Databases: []db{
			{
				Id:   "test1",
				Path: ":memory:",
				InitStatements: []string{
					"CREATE TABLE T (TXT TEXT)",
				},
			},
		},
	}

	go launch(cfg, true)

	time.Sleep(time.Second)

	req1 := request{
		Transaction: []requestItem{
			{
				Statement: "INSERT INTO T VALUES ('世界')",
			},
		},
	}
	req2 := request{
		Transaction: []requestItem{
			{
				Query: "SELECT TXT FROM T",
			},
		},
	}

	code, body, _ := call("test1", req1, t)
	if code != 200 {
		t.Error("INSERT failed", body)
	}

	code, body, res := call("test1", req2, t)
	if code != 200 {
		t.Error("SELECT failed", body)
	}
	if res.Results[0].ResultSet[0]["TXT"] != "世界" {
		t.Error("Unicode extraction failed", body)
	}

	time.Sleep(time.Second)

	Shutdown()
}

func TestFailBegin(t *testing.T) {
	cfg := config{
		Bindhost: "0.0.0.0",
		Port:     12321,
		Databases: []db{
			{
				Id:   "test1",
				Path: ":memory:",
				InitStatements: []string{
					"CREATE TABLE T (TXT TEXT)",
				},
			},
		},
	}

	go launch(cfg, true)

	time.Sleep(time.Second)

	req := request{
		Transaction: []requestItem{
			{
				NoFail:    true,
				Statement: "BEGIN",
			},
			{
				NoFail:    true,
				Statement: "COMMIT",
			},
			{
				NoFail:    true,
				Statement: "ROLLBACK",
			},
		},
	}

	code, _, res := call("test1", req, t)
	if code != 200 {
		t.Error("request failed, but shouldn't have")
	}

	if res.Results[0].Success {
		t.Error("BEGIN succeeds, but shouldn't have")
	}
	if res.Results[1].Success {
		t.Error("COMMIT succeeds, but shouldn't have")
	}
	if res.Results[2].Success {
		t.Error("ROLLBACK succeeds, but shouldn't have")
	}

	time.Sleep(time.Second)

	Shutdown()
}

func TestExoticSuffixes(t *testing.T) {
	os.Remove("../test/test.sqlite3")
	defer os.Remove("../test/test.sqlite3")

	cfg := config{
		Bindhost: "0.0.0.0",
		Port:     12321,
		Databases: []db{
			{
				Id:   "test",
				Path: "../test/test.sqlite3",
				StoredStatement: []storedStatement{
					{
						Id:  "Q",
						Sql: "SELECT 1",
					},
				},
			},
		},
	}
	go launch(cfg, true)

	time.Sleep(time.Second)

	if !fileExists("../test/test.sqlite3") {
		t.Error("db file not created")
		return
	}

	req := request{
		Transaction: []requestItem{
			{
				Statement: "CREATE TABLE T1 (ID INT PRIMARY KEY, VAL TEXT NOT NULL)",
			},
		},
	}

	code, _, _ := call("test", req, t)

	if code != 200 {
		t.Error("did not succeed")
	}

	time.Sleep(time.Second)

	Shutdown()
}

func TestFileServer(t *testing.T) {
	serveDir := "../test/"
	cfg := config{
		Bindhost: "0.0.0.0",
		Port:     12321,
		ServeDir: &serveDir,
	}
	go launch(cfg, true)
	time.Sleep(time.Second)
	client := &fiber.Client{}
	get := client.Get("http://localhost:12321/mem1.yaml")

	code, _, errs := get.String()

	if errs != nil && len(errs) > 0 {
		t.Error(errs[0])
	}

	if code != 200 {
		t.Error("did not succeed")
	}

	time.Sleep(time.Second)

	Shutdown()
}

func TestFileServerKO(t *testing.T) {
	serveDir := "../test/"
	cfg := config{
		Bindhost: "0.0.0.0",
		Port:     12321,
		ServeDir: &serveDir,
	}
	go launch(cfg, true)
	time.Sleep(time.Second)
	client := &fiber.Client{}
	get := client.Get("http://localhost:12321/mem1_nonexistent.yaml")

	code, _, errs := get.String()

	if errs != nil && len(errs) > 0 {
		t.Error(errs[0])
	}

	if code != 404 {
		t.Error("did not fail")
	}

	time.Sleep(time.Second)

	Shutdown()
}

func TestFileServerWithOverlap(t *testing.T) {
	serveDir := "../test/"
	cfg := config{
		Bindhost: "0.0.0.0",
		Port:     12321,
		ServeDir: &serveDir,
	}
	go launch(cfg, true)
	time.Sleep(time.Second)
	client := &fiber.Client{}
	get := client.Get("http://localhost:12321/test1")

	code, _, errs := get.String()

	if errs != nil && len(errs) > 0 {
		t.Error(errs[0])
	}

	if code != 200 {
		t.Error("did not succeed")
	}

	time.Sleep(time.Second)

	Shutdown()
}
