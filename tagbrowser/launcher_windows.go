package tagbrowser

import "os/exec"

func launch(file string, line int) {
	cmd := exec.Command("notepad/notepad++", file, "-n", line)
	//cmd := exec.Command("vim", file, fmt.Sprintf("+%v", line))
	cmd.Run()
}
