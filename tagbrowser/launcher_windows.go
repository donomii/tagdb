package tagbrowser

import "os/exec"

func Launch(file string, line string) {
	cmd := exec.Command("notepad/notepad++", file, "-n", line)
	//cmd := exec.Command("vim", file, fmt.Sprintf("+%v", line))
	cmd.Run()
}
