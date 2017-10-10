package tagbrowser

import "os"
import "os/exec"

func Launch(file string, line string) {
	cmd := exec.Command("notepad/notepad++", file, "-n", line)
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	//cmd := exec.Command("vim", file, fmt.Sprintf("+%v", line))
	cmd.Run()
}
