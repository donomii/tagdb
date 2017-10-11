package tagbrowser

import "os"
import "os/exec"
import "fmt"

func Launch(file string, line string) {
	cmd := exec.Command("bash", "-c", fmt.Sprintf("vim %v %v", results[selection].Filename, fmt.Sprintf("+%v", results[selection].Line)))
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Run()
}
