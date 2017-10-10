package tagbrowser

import "os"
import "os/exec"
import "fmt"

func Launch(file string, line string) {
	cmd := exec.Command("vim", file, fmt.Sprintf("+%v", line))
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Run()
}
