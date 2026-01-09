package main

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"time"

	"github.com/brotherlogic/goserver/utils"

	pb "github.com/brotherlogic/recorder/proto"
	pbrg "github.com/brotherlogic/recordgetter/proto"
)

type Recorder struct {
	cmd *exec.Cmd
}

type Server struct {
	r *Recorder
}

func getCurrentRecord() (int32, int32, error) {
	ctx, cancel := utils.ManualContext("recorder-get", time.Minute)
	defer cancel()

	conn, err := utils.LFDialServer(ctx, "recordgetter")
	if err != nil {
		log.Fatalf("Can't dial getter: %v", err)
	}
	defer conn.Close()
	client := pbrg.NewRecordGetterClient(conn)

	curr, err := client.GetRecord(ctx, &pbrg.GetRecordRequest{
		Type: pbrg.RequestType_DEFAULT,
	})

	if err != nil {
		return -1, -1, err
	}

	disk := curr.GetDisk()
	if curr.GetRecord().GetRelease().GetFormatQuantity() == 1 {
		disk = 0
	}

	return curr.GetRecord().GetRelease().GetId(), disk, nil
}

func (r *Recorder) runRecord() error {
	num, disk, err := getCurrentRecord()
	if err != nil {
		return err
	}

	// Start recording
	date := time.Now().Format("2006-01-02")
	diskRef := fmt.Sprintf("%v_%v-%v.wav", num, disk, date)
	if disk == 0 {
		diskRef = fmt.Sprintf("%v-%v.wav", num, date)
	}

	r.cmd = exec.Command("arecord", "--device", "\"hw:2,0\"", "--format", "S32_LE", "--rate", "44100", "--channels", "4", diskRef)
	output, err := r.cmd.CombinedOutput()
	log.Printf("Error: %v -> %v", err, string(output))
	r.cmd.Wait()

	return nil
}

func (s *Server) NewRecord(ctx context.Context, _ *pb.NewRecordRequest) {
	// Halt the current recording
	c := exec.Command("killall", "arecord")
	c.Start()
	c.Wait()
}

func main() {
	go func() {
		r := &Recorder{}
		err := r.runRecord()
		log.Printf("Error recording: %v", err)
		time.Sleep(time.Second * 5)
	}()

	for {
		time.Sleep(time.Second)
	}
}
