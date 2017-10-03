package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
)

func main() {
	// init process
	genStationMap()
	date := flag.String("date", time.Now().Format("2006-01-02"), "ticket date, format: YYYY-MM-DD")
	fromc := flag.String("from", "AOH", fmt.Sprintf("department station code, available: %s", staMsg))
	toc := flag.String("to", "CZH", "arrival station code, available: same as department station")
	flag.Parse()
	defer glog.Flush()
	okd, _ := regexp.Match(`^\d\d\d\d-\d\d-\d\d$`, []byte(*date))
	if !okd {
		glog.V(0).Infof("wrong input date format, should be YYYY-MM-DD")
		return
	}
	fromn, okf := c2nStaMap[cSta(*fromc)]
	ton, okt := c2nStaMap[cSta(*toc)]
	if !okf || !okt {
		glog.V(0).Infof("unsupport input station, available: %s", staMsg)
		return
	}
	glog.V(0).Infof("*** Want ticket at %s@%s->%s ***", *date, fromn, ton)

	// query ticket
	tks, err := queryTicket(*date, cSta(*fromc), cSta(*toc))
	if err != nil {
		glog.V(0).Infof("query ticket: %v", err)
		return
	}
	for _, tk := range tks {
		if tk.SecondCls != 0 || tk.StandCls != 0 {
			glog.V(0).Infof("T** %s", tk)
		}
	}
}

// queryTicket query tickets with more accuration based on query12306
func queryTicket(date string, fromc cSta, toc cSta) ([]Ticket, error) {
	start := time.Now()
	api := "http://kyfw.12306.cn/otn/leftTicket/queryX?leftTicketDTO.train_date=%s&leftTicketDTO.from_station=%s&leftTicketDTO.to_station=%s&purpose_codes=ADULT"
	url := fmt.Sprintf(api, date, fromc, toc)
	glog.V(1).Infof("query url %s", url)
	const maxWait = 50 * time.Second
	wait := 50 * time.Millisecond
	var rtks, tks []Ticket
	var err error
	for rtks, err = query12306(url); time.Since(start) < maxWait &&
		err != nil; rtks, err = query12306(url) {
		glog.V(1).Infof("wait %s then try again, last error: %v", wait, err)
		time.Sleep(wait)
		wait *= 2
	}
	if err != nil {
		return nil, fmt.Errorf("exceed max wait limit [%s], unable to query tickets after %s", maxWait, time.Since(start))
	}
	glog.V(1).Info("*** A successful query ***")
	for _, tk := range rtks {
		if tk.From == fromc && tk.To == toc {
			tks = append(tks, tk)
		}
	}
	return tks, nil
}

// query tickets from kyfw.12306.cn, using train date formats like 'YYYY-MM-DD' and from & to station name.
func query12306(url string) ([]Ticket, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("http get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("response status code: %s", resp.Status)
	}
	glog.V(2).Infof("header: %s", resp.Header)
	if resp.Header.Get("Content-Type") != "application/json;charset=UTF-8" {
		return nil, fmt.Errorf("content type not 'application/json;charset=UTF-8'")
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %v", err)
	}
	glog.V(2).Infof("body: %s", string(body))
	raw := new(struct {
		Data     struct{ Result []string }
		Messages []string
	})
	err = json.Unmarshal(body, raw)
	if err != nil {
		return nil, fmt.Errorf("decode json: %v", err)
	}
	if len(raw.Messages) > 0 {
		return nil, fmt.Errorf("message: %s", strings.Join(raw.Messages, ";"))
	}
	tks := make([]Ticket, 0)
	for _, line := range raw.Data.Result {
		tks = append(tks, mkticket(line))
	}
	return tks, nil
}

// make ticket out of raw string line
func mkticket(line string) Ticket {
	var tk Ticket
	glog.V(2).Infof("make ticket out of: %s", line)
	tab := strings.Split(line, "|")
	tk.Train = tab[3]
	tk.Stat = tab[1]
	tk.From = cSta(tab[6])
	tk.To = cSta(tab[7])
	tk.Depart = atot("20060102 15:04", fmt.Sprintf("%s %s", tab[13], tab[8]))
	tk.Arrive = atot("20060102 15:04", fmt.Sprintf("%s %s", tab[13], tab[9]))
	tk.SecondCls = atoitk(tab[30])
	tk.StandCls = atoitk(tab[26])
	glog.V(2).Infof("F** %s", tk)
	return tk
}

// atot convert ascii to time.Time
func atot(f, s string) time.Time {
	tm, err := time.Parse(f, strings.Replace(s, "24:00", "23:59", 1))
	if err != nil {
		glog.V(2).Infof("atot: %v", err)
	}
	return tm
}

// atoitk convert ascii to ticket number
func atoitk(s string) int {
	if s == "有" {
		return -1
	}
	if s == "" || s == "无" {
		return 0
	}
	i, err := strconv.Atoi(s)
	if err != nil {
		glog.V(2).Infof("atoitk: %v", err)
	}
	return i
}

// atoitk convert ascii to ticket number
func itktoa(i int) string {
	if i == -1 {
		return "有"
	}
	if i == 0 {
		return "无"
	}
	return strconv.Itoa(i)
}

// data type for station
type nSta string // Chinese station name
type cSta string // English station code
var n2cStaMap map[nSta]cSta
var c2nStaMap map[cSta]nSta
var staMsg string

//genStationMap make convertion map between station name and code
func genStationMap() {
	n2cStaMap = map[nSta]cSta{
		"上海虹桥": "AOH",
		"上海":   "SHH",
		"上海南":  "SNH",
		"常州":   "CZH",
		"常州北":  "ESH",
		"戚墅堰":  "QYH",
	}
	c2nStaMap = make(map[cSta]nSta)
	var staBuf bytes.Buffer
	for n, c := range n2cStaMap {
		c2nStaMap[c] = n
		staBuf.WriteString(fmt.Sprintf("%s-%s;", c, n))
	}
	staMsg = staBuf.String()
}

// data type for ticket information
type Ticket struct {
	Train     string
	Stat      string
	From      cSta
	To        cSta
	Depart    time.Time
	Arrive    time.Time
	SecondCls int
	StandCls  int
}

// ticket display
func (t Ticket) String() string {
	return fmt.Sprintf("[%s][%-5s] [时刻]%s->%s [车站]%s->%s [座位]二等:%s/站票:%s",
		t.Stat, t.Train, t.Depart.Format("15:04"), t.Arrive.Format("15:04"),
		c2nStaMap[t.From], c2nStaMap[t.To], itktoa(t.SecondCls), itktoa(t.StandCls))
}
