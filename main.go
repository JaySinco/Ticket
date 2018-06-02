package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func main() {
	if len(os.Args[1:]) != 3 || len(os.Args[3]) != 6 {
		fmt.Println("Usage: ticket [from] [to] [YYMMDD]")
		return
	}
	datetm, err := time.Parse("060102", os.Args[3])
	if err != nil {
		fmt.Printf("[ERROR] failed to parse date '%s', should be in form 'YYMMDD'\n", os.Args[3])
		return
	}
	date := datetm.Format("2006-01-02")

	stas, err := getStationSet()
	if err != nil {
		fmt.Printf("[ERROR] get station set: %v\n", err)
		return
	}
	from := pickStation(stas, os.Args[1])
	to := pickStation(stas, os.Args[2])
	if from.name == "" || to.name == "" {
		fmt.Println("[ERROR] failed to resolve station pattern")
		return
	}
	tks, err := query12306(date, from, to, stas)
	if err != nil {
		fmt.Printf("[ERROR] query 12306: %v\n", err)
		return
	}
	fmt.Printf("[QUERY] %s从%s开往%s尚有余票的列车如下：\n", datetm.Format("2006年01月02日"), from.name, to.name)
	fmt.Println("        车次     出发   到达   二等座   无座")
	fmt.Println("        -------------------------------------")
	for _, tk := range tks {
		fmt.Printf("        %-5s   %s   %s   %s\t%s\n", tk.train, tk.depart.Format("15:04"), tk.arrive.Format("15:04"),
			itktoa(tk.secondCls), itktoa(tk.standCls))
	}
}

func pickStation(stas staSet, pattern string) (sta station) {
	switch opts := stas.findByNameOrAbbrev(pattern); len(opts) {
	case 0:
	case 1:
		sta = opts[0]
	default:
		fmt.Printf("[CHOSE] '%s'有多个匹配的车站如下：\n", pattern)
		for i, s := range opts {
			if i%2 == 0 {
				fmt.Printf("      % 4d - %s", i, s.name)
				if i == len(opts)-1 {
					fmt.Printf("\n")
				}
			} else {
				fmt.Printf("\t\t\t% 4d - %s\n", i, s.name)
			}
		}
		fmt.Printf("[ENTER] 上列车站中对应'%s'的序号：", pattern)
		reader := bufio.NewReader(os.Stdin)
		index, _, _ := reader.ReadLine()
		if picked, err := strconv.ParseUint(string(index), 10, 64); err == nil && int(picked) < len(opts) {
			sta = opts[picked]
		}
	}
	return
}

func query12306(date string, from, to station, stas staSet) ([]*ticket, error) {
	api := "https://kyfw.12306.cn/otn/leftTicket/query?leftTicketDTO.train_date=%s&leftTicketDTO.from_station=%s&leftTicketDTO.to_station=%s&purpose_codes=ADULT"
	url := fmt.Sprintf(api, date, from.code, to.code)
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("https get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("response status code: %s", resp.Status)
	}
	if resp.Header.Get("Content-Type") != "application/json;charset=UTF-8" {
		return nil, fmt.Errorf("content type not 'application/json;charset=UTF-8'")
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %v", err)
	}
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
	tks := make([]*ticket, 0)
	for _, line := range raw.Data.Result {
		tk := mkticket(line, stas)
		if tk.from == from && tk.to == to && (tk.secondCls != 0 || tk.standCls != 0) {
			tks = append(tks, tk)
		}
	}
	return tks, nil
}

func mkticket(line string, stas staSet) *ticket {
	var tk ticket
	tab := strings.Split(line, "|")
	tk.train = tab[3]
	tk.stat = tab[1]
	tk.from = stas.findByCode(tab[6])
	tk.to = stas.findByCode(tab[7])
	tk.depart = atot("20060102 15:04", fmt.Sprintf("%s %s", tab[13], tab[8]))
	tk.arrive = atot("20060102 15:04", fmt.Sprintf("%s %s", tab[13], tab[9]))
	tk.secondCls = atoitk(tab[30])
	tk.standCls = atoitk(tab[26])
	return &tk
}

type ticket struct {
	train     string
	stat      string
	from      station
	to        station
	depart    time.Time
	arrive    time.Time
	secondCls int
	standCls  int
}

func (t ticket) String() string {
	return fmt.Sprintf("[%s][%-5s] [时刻]%s~%s [车站]%s->%s [座位]二等:%s/站票:%s",
		t.stat, t.train, t.depart.Format("15:04"), t.arrive.Format("15:04"),
		t.from.name, t.to.name, itktoa(t.secondCls), itktoa(t.standCls))
}

func atot(f, s string) time.Time {
	tm, _ := time.Parse(f, strings.Replace(s, "24:00", "23:59", 1))
	return tm
}

func atoitk(s string) int {
	if s == "有" {
		return -1
	}
	if s == "" || s == "无" || s == "*" {
		return 0
	}
	i, _ := strconv.Atoi(s)
	return i
}

func itktoa(i int) string {
	if i == -1 {
		return "有"
	}
	if i == 0 {
		return "无"
	}
	return strconv.Itoa(i)
}

func getStationSet() (stas staSet, err error) {
	api := "https://kyfw.12306.cn/otn/resources/js/framework/station_name.js?station_version=1.9055"
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}}
	resp, err := client.Get(api)
	if err != nil {
		return nil, fmt.Errorf("https get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("response status code: %s", resp.Status)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if ok := bytes.HasPrefix(body, []byte("var station_names ='")); !ok {
		return nil, fmt.Errorf("wrong prefix within response body")
	}
	raw := string(body[21 : len(body)-2])
	rawlst := strings.Split(raw, "@")
	for _, r := range rawlst {
		e := strings.Split(r, "|")
		stas = append(stas, station{e[1], e[2], e[3], e[4]})
	}
	return stas, nil
}

type staSet []station

func (m staSet) findByCode(code string) (sta station) {
	for _, s := range m {
		if s.code == code {
			sta = s
		}
	}
	return
}

func (m staSet) findByNameOrAbbrev(pattern string) (stas staSet) {
	for _, s := range m {
		nok, _ := regexp.MatchString(pattern, s.name)
		aok, _ := regexp.MatchString(pattern, s.abbrev)
		if nok || aok {
			stas = append(stas, s)
		}
	}
	return stas
}

type station struct {
	name   string
	code   string
	pinyin string
	abbrev string
}

func (s station) String() string {
	return fmt.Sprintf("[%s]%s(%s|%s)", s.code, s.name, s.pinyin, s.abbrev)
}
