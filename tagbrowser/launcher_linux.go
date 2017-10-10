package tagbrowser

import "os/exec"
import "fmt"

func Launch(file string, line string) {
	cmd := exec.Command("vim", file, fmt.Sprintf("+%v", line))
	cmd.Run()
}
