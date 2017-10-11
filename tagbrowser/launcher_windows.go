package tagbrowser

import "os"
import "fmt"
import "os/exec"

func Launch(file string, line string) {
	cmd := exec.Command("notepad/notepad++", file, fmt.Sprintf("-n%v", line))
	//cmd := exec.Command("C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe", results[selection].Filename)
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Run()
}
