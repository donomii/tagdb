package tagbrowser

import "os/exec"
import "fmt"

func launch(file string, line int) {
	cmd := exec.Command("vim", file, fmt.Sprintf("+%v", line))
	cmd.Run()
}
