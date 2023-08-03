package utils

import (
	"bufio"
	"github.com/ttacon/chalk"
	"log"
	"os"
)

func UniqueUrls(strSlice []string) []string {
	keys := make(map[string]bool)
	list := []string{}
	for _, entry := range strSlice {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list

}

func AppendToFile(filename string, text string) {
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Println(chalk.Red.Color("error: " + filename + "文件创建/打开失败, " + err.Error()))
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	_, err = w.WriteString(text + "\n")
	if err != nil {
		log.Println(chalk.Red.Color("error: " + text + "写入" + filename + "文件失败, " + err.Error()))
	}
	w.Flush()
}
func GetUrlListFromTxt(txtPath string) []string {

	var txtlines []string
	if txtPath != "" {
		file, err := os.Open(txtPath)
		if err != nil {
			log.Println(chalk.Red.Color("error: failed to open file: " + err.Error()))
		}
		scanner := bufio.NewScanner(file)
		scanner.Split(bufio.ScanLines)
		for scanner.Scan() {
			txtlines = append(txtlines, scanner.Text())
		}
		file.Close()
	}
	return txtlines
}
