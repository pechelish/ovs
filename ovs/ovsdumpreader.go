package ovs

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type OvsDumpSource interface {
	TunDumpFlows(ip string, port int) ([]string, error)
	TunDumpPorts(ip string, port int) ([]string, error)
	ExDumpFlows(ip string, port int) ([]string, error)
	ExDumpPorts(ip string, port int) ([]string, error)
	IntDumpFlows(ip string, port int) ([]string, error)
	IntDumpPorts(ip string, port int) ([]string, error)
//	DumpGroups(ip string, port int) ([]string, error)
//	DumpGroupStats(ip string, port int) ([]string, error)
}

type OvsDumpReader struct {
	dumpSource OvsDumpSource
}

var (
	flowLine       *regexp.Regexp = regexp.MustCompile("cookie=(?P<cookie>[^,]*), duration=(?P<duration>[^,]*)s, table=(?P<table>[^,]*), n_packets=(?P<packets>[^,]*), n_bytes=(?P<bytes>[^,]*), idle_age=(?P<idle_age>[^,]*), hard_age=(?P<hard_age>[^,]*), priority=(?P<priority>[^,]*)(,(?P<match>[^ ]*))? actions=(?P<actions>.*)")
	portLine       *regexp.Regexp = regexp.MustCompile(`port\s*(?P<port>[^:]*):\srx\spkts=(?P<rxpackets>[^,]*),\sbytes=(?P<rxbytes>[^,]*),\sdrop=(?P<rxdrops>[^,]*),\serrs=(?P<rxerrors>[^,]*),\sframe=(?P<rxframerr>[^,]*),\sover=(?P<rxoverruns>[^,]*),\scrc=(?P<rxcrcerrors>[^,]*)\s.*tx\spkts=(?P<txpackets>[^,]*),\sbytes=(?P<txbytes>[^,]*),\sdrop=(?P<txdrops>[^,]*),\serrs=(?P<txerrors>[^,]*),\scoll=(?P<txcollisions>.*)`)
	groupsLine     *regexp.Regexp = regexp.MustCompile(`group_id=(?P<groupid>.*?),\s*type=(?P<type>[^,]*),bucket=(?P<buckets>.*$)`)
	bucketAction   *regexp.Regexp = regexp.MustCompile("actions=(.*?),?$")
	groupStatsLine *regexp.Regexp = regexp.MustCompile(`group_id=(?P<groupid>.*?),duration=(?P<duration>[^,]*)s,(?P<counts>.*$)`)
	countLine      *regexp.Regexp = regexp.MustCompile("ref_count=(?P<ref_count>[0-9]+),packet_count=(?P<packet_count>[0-9]+),byte_count=(?P<byte_count>[0-9]+).*")
	CliDumpReader  OvsDumpReader  = OvsDumpReader{OvsDumpSourceCLI{}}
)

func getRegexpMap(match []string, names []string) map[string]string {
	result := make(map[string]string, len(match))
	for i, m := range match {
		result[names[i]] = m
	}
	return result
}

func parseOpenFlowFlowDumpLine(line string) (Flow, error) {
	match := flowLine.FindStringSubmatch(line)
	result := getRegexpMap(match, flowLine.SubexpNames())
	//TODO: we need to consider if len(result) == 0 is an error or business as usual
	duration, _ := strconv.ParseFloat(result["duration"], 64)
	packets, _ := strconv.Atoi(result["packets"])
	bytes, _ := strconv.Atoi(result["bytes"])
	idleage, _ := strconv.Atoi(result["idle_age"])
	hardage, _ := strconv.Atoi(result["hard_age"])

	flow := Flow{
		Cookie:      result["cookie"],
		Duration:    duration,
		Table:       result["table"],
		Packets:     packets,
		Bytes:       bytes,
		IdleAge:	 idleage,
		HardAge:	 hardage,
		Priority:    result["priority"],
		Match:       result["match"],
		Action:      result["actions"],
	}
	if len(result) == 0 {
		return flow, errors.New("exec: Stdout already empty");
	}
	if result["match"] != "" && result["actions"] != "" && result["table"] != "" && result["priority"] != "" {
		return flow, nil
	}
	return flow, errors.New("exec: Stdout already empty");
}

func parseOpenFlowPortDumpLine(first_line, second_line string) (Port, error) {
	line := first_line + second_line
	line = strings.ReplaceAll(line, "=?", "=0")
	line = strings.ReplaceAll(line, "\"", "")
	match := portLine.FindStringSubmatch(line)
	result := getRegexpMap(match, portLine.SubexpNames())
	rxpackets, _ := strconv.Atoi(result["rxpackets"])
	txpackets, _ := strconv.Atoi(result["txpackets"])
	rxbytes, _ := strconv.Atoi(result["rxbytes"])
	txbytes, _ := strconv.Atoi(result["txbytes"])
	rxdrops, _ := strconv.Atoi(result["rxdrops"])
	txdrops, _ := strconv.Atoi(result["txdrops"])

	port := Port{
		PortNumber:   result["port"],
		RxPackets:    rxpackets,
		TxPackets:    txpackets,
		RxBytes:      rxbytes,
		TxBytes:      txbytes,
		RxDrops:      rxdrops,
		TxDrops:      txdrops,
		RxErrors:     result["rxerrors"],
		TxErrors:     result["txerrors"],
		RxFrameErr:   result["rxframerr"],
		RxOverruns:   result["rxoverruns"],
		RxCrcErrors:  result["rxcrcerrors"],
		TxCollisions: result["txcollisions"],
	}

	if len(result) == 0 {
		return port, errors.New("exec: Stdout already empty");
	}
	if result["port"] != "" {
		return port, nil
	}
	return port, errors.New("exec: Stdout already empty");
}

func parseOpenFlowGroupsDumpLine(line string) Group {
	match := groupsLine.FindStringSubmatch(line)
	result := getRegexpMap(match, groupsLine.SubexpNames())

	group := Group{
		GroupId:   result["groupid"],
		GroupType: result["type"],
	}

	if len(result["buckets"]) > 0 {
		//Split the group line into buckets
		buckets := strings.Split(result["buckets"], "bucket=")
		bucketEntries := make([]Bucket, len(buckets))
		for idx, bucket := range buckets {
			subMatch := bucketAction.FindStringSubmatch(bucket)
			if len(subMatch) > 1 {
				bucketEntries[idx].Actions = subMatch[1]
			}
		}
		group.Buckets = bucketEntries
	}

	return group
}

func parseOpenFlowGroupStatsDumpLine(line string, groupIdMap map[string]*Group) {
	match := groupStatsLine.FindStringSubmatch(line)
	result := getRegexpMap(match, groupStatsLine.SubexpNames())

	var group *Group = groupIdMap[result["groupid"]]
	group.Duration, _ = strconv.Atoi(result["duration"])
	bucketCounts := strings.Split(result["counts"], ":")

	//The 0th element in this split should contain the aggregated packet/byte counter for the whole group
	subMatch := countLine.FindStringSubmatch(bucketCounts[0])
	subResult := getRegexpMap(subMatch, countLine.SubexpNames())
	group.Packets, _ = strconv.Atoi(subResult["packet_count"])
	group.Bytes, _ = strconv.Atoi(subResult["byte_count"])

	//The others should contain bucket data
	for j := 1; j < len(bucketCounts); j++ {
		bucketMatch := countLine.FindStringSubmatch(bucketCounts[0])
		bucketResult := getRegexpMap(bucketMatch, countLine.SubexpNames())
		group.Buckets[j-1].Packets, _ = strconv.Atoi(bucketResult["packet_count"])
		group.Buckets[j-1].Bytes, _ = strconv.Atoi(bucketResult["byte_count"])
	}
}

func (o OvsDumpReader) TunFlows(ip string, port int) ([]Flow, error) {
	lines, err := o.dumpSource.TunDumpFlows(ip, port)
	//if error was occured we return
	if err != nil {
		return nil, err
	}
	entrySet := make([]Flow, len(lines))
	counter := 0;
	for i := 0; i < len(lines); i++ {
		flowEntry, err := parseOpenFlowFlowDumpLine(lines[i])
		if err == nil {
			entrySet[counter] = flowEntry;
			counter++;
		}
	}
	retrySet := make([]Flow, counter)
	for i := 0; i < counter; i++ {
		retrySet[i] = entrySet[i];
	}
	fmt.Println(retrySet)

	return retrySet, nil
}

func (o OvsDumpReader) ExFlows(ip string, port int) ([]Flow, error) {
	lines, err := o.dumpSource.ExDumpFlows(ip, port)
	//if error was occured we return

	if err != nil {
		return nil, err
	}
	entrySet := make([]Flow, len(lines))
	counter := 0;
	for i := 0; i < len(lines); i++ {
		flowEntry, err := parseOpenFlowFlowDumpLine(lines[i])
		if err == nil {
			entrySet[counter] = flowEntry;
			counter++;
		}
	}
	retrySet := make([]Flow, counter)
	for i := 0; i < counter; i++ {
		retrySet[i] = entrySet[i];
	}
	fmt.Println(retrySet)

	return retrySet, nil
}

func (o OvsDumpReader) IntFlows(ip string, port int) ([]Flow, error) {
	lines, err := o.dumpSource.IntDumpFlows(ip, port)
	//if error was occured we return

	if err != nil {
		return nil, err
	}
	entrySet := make([]Flow, len(lines))
	counter := 0;
	for i := 0; i < len(lines); i++ {
		flowEntry, err := parseOpenFlowFlowDumpLine(lines[i])
		if err == nil {
			entrySet[counter] = flowEntry;
			counter++;
		}
	}
	retrySet := make([]Flow, counter)
	for i := 0; i < counter; i++ {
		if err == nil {
		retrySet[i] = entrySet[i];
	}
	}
	fmt.Println(retrySet)

	return retrySet, nil
}

func (o OvsDumpReader) TunPorts(ip string, port int) ([]Port, error) {
	lines, err := o.dumpSource.TunDumpPorts(ip, port)
	//if error was occured we return
	if err != nil {
		return nil, err
	}

	entrySet := make([]Port, int(len(lines)/2))
	for i := 0; i < len(lines); i += 2 {
		entry, err := parseOpenFlowPortDumpLine(lines[i], lines[i+1])
		if err == nil {
		entrySet[int(i/2)] = entry
	}
	}
	fmt.Println(entrySet)

	return entrySet, nil
}

func (o OvsDumpReader) ExPorts(ip string, port int) ([]Port, error) {
	lines, err := o.dumpSource.ExDumpPorts(ip, port)
	//if error was occured we return
	if err != nil {
		return nil, err
	}

	entrySet := make([]Port, int(len(lines)/2))
	counter := 0;
	for i := 0; i < len(lines); i+=2 {
		flowEntry, err := parseOpenFlowPortDumpLine(lines[i], lines[i+1])
		if err == nil {
			entrySet[counter] = flowEntry;
			counter++;
		}
	}
	retrySet := make([]Port, counter)
	for i := 0; i < counter; i++ {
		if err == nil {
		retrySet[i] = entrySet[i];
	}
	}
	fmt.Println(retrySet)

	return retrySet, nil
}

func (o OvsDumpReader) IntPorts(ip string, port int) ([]Port, error) {
	lines, err := o.dumpSource.IntDumpPorts(ip, port)
	//if error was occured we return
	if err != nil {
		return nil, err
	}

	entrySet := make([]Port, int(len(lines)/2));
	counter := 0;
	for i := 0; i < len(lines); i += 2 {
		if i+1 < len(lines) {
			entry, err := parseOpenFlowPortDumpLine(lines[i], lines[i+1])
			if err == nil {
				entrySet[counter] = entry;
				counter++;
			}
		}
	}
	retrySet := make([]Port, counter)
	for i := 0; i < counter; i++ {
		retrySet[i] = entrySet[i];
	}
	fmt.Println(retrySet)

	return retrySet, nil
}

//func (o OvsDumpReader) Groups(ip string, port int) ([]Group, error) {
//	groupLines, err := o.dumpSource.DumpGroups(ip, port)
//
//	//if error was occured we return
//	if err != nil {
//		return nil, err
//	}
//
//	groupStatLines, err := o.dumpSource.DumpGroupStats(ip, port)
//	//if error was occured we return
//	if err != nil {
//		return nil, err
//	}
//
//	//if command was succesfull we further parse the output
//	groupEntries := make([]Group, len(groupLines))
//	groupIdMap := make(map[string]*Group)
//
//	for i, line := range groupLines {
//		groupEntry := parseOpenFlowGroupsDumpLine(line)
//		groupEntries[i] = groupEntry
//		groupIdMap[groupEntry.GroupId] = &groupEntries[i]
//	}
//
//	for _, line := range groupStatLines {
//		parseOpenFlowGroupStatsDumpLine(line, groupIdMap)
//	}
//
//	return groupEntries, nil
//}
