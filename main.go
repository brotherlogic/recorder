package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/brotherlogic/goserver/utils"
	"google.golang.org/grpc"

	pb "github.com/brotherlogic/recorder/proto"
	pbrg "github.com/brotherlogic/recordgetter/proto"
)

var (
	port    = flag.Int("port", 8080, "Port to server from")
	procDir = flag.String("processing_dir", "/home/simon/processing/", "Directory to processing recordings")
	saveDir = flag.String("save_dir", "/home/simon/music/flacs/", "Directory to save recordings")
)

type Recorder struct {
	cmd   *exec.Cmd
	pLock sync.Mutex
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

func (s *Server) processFiles(dir string) error {
	// Lock to prevent stomping
	s.r.pLock.Lock()
	defer s.r.pLock.Unlock()

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, file := range entries {
		log.Printf("Processing file: %v", file.Name())

		if file.IsDir() || !strings.HasSuffix(file.Name(), ".wav") {
			log.Printf("INVALID FILE: %v", file.Name())
			continue
		}

		strippedFile := file.Name()[:len(file.Name())-4]
		elems := strings.Split(file.Name(), "-")
		selems := strings.Split(elems[0], "_")

		// Process the file
		soxCmd := exec.Command("sox", fmt.Sprintf("%v%v", *procDir, file.Name()), fmt.Sprintf("%v%v_track_.wav", *procDir, strippedFile), "silence", "1", "0.5", "1%", "1", "0.5", "1%", ":", "newfile", ":", "restart")
		log.Printf("Running sox command: %v", soxCmd.String())
		output, err := soxCmd.CombinedOutput()
		log.Printf("Sox output: %v -> %v", err, string(output))
		if err != nil {
			return err
		}

		// Convert to flac
		files, err := filepath.Glob(fmt.Sprintf("%v%v_track_*.wav", *procDir, strippedFile))
		if err != nil {
			return err
		}
		args := []string{"--best", "--delete-input-file", "--output-prefix", *procDir}
		args = append(args, files...)
		flacCmd := exec.Command("flac", args...)
		log.Printf("Running flac command: %v", flacCmd.String())
		output, _ = flacCmd.CombinedOutput()
		log.Printf("Flac output: %v -> %v", err, string(output))

		//Move file into save dir - we don't care if this fails
		err = os.Mkdir(fmt.Sprintf("%v/%v", *saveDir, selems[0]), 0755)
		log.Printf("Error in mkdir: %v", err)
		files, err = filepath.Glob(fmt.Sprintf("%v%v*track*.flac", *procDir, selems[0]))
		if err != nil {
			return err
		}
		args = append([]string{}, files...)
		args = append(args, fmt.Sprintf("%v/%v/", *saveDir, selems[0]))
		moveCmd := exec.Command("mv", args...)
		log.Printf("Running move command: %v", moveCmd.String())
		output, err = moveCmd.CombinedOutput()
		log.Printf("Move output: %v -> %v", err, string(output))
		if err != nil {
			return err
		}

		files, err = filepath.Glob(fmt.Sprintf("%v%v*.wav", *procDir, strippedFile))
		if err != nil {
			return err
		}
		rmCmd := exec.Command("rm", append([]string{}, files...)...)
		out, err := rmCmd.CombinedOutput()
		log.Printf("RM %v -> %v", err, string(out))
	}

	return nil
}

func (r *Recorder) runRecord() error {
	log.Printf("Running record")
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

	r.cmd = exec.Command("arecord", "--device", "hw:2,0", "--format", "S32_LE", "--rate", "44100", "--channels", "4", diskRef)
	log.Printf("Starging record")
	output, err := r.cmd.CombinedOutput()
	log.Printf("Error: %v -> %v", err, string(output))
	r.cmd.Wait()

	// Move all the files over to the processing directory
	moveCmd := exec.Command("mv", fmt.Sprintf("%v*.wav", num), *procDir)
	log.Printf("Copying files")
	output, err = moveCmd.CombinedOutput()
	log.Printf("Moved files %v -> %v", err, string(output))

	if err != nil {
		return err
	}

	return nil
}

func (s *Server) NewRecord(ctx context.Context, _ *pb.NewRecordRequest) (*pb.NewRecordResponse, error) {
	// Halt the current recording
	c := exec.Command("killall", "arecord")
	c.Start()
	c.Wait()
	return &pb.NewRecordResponse{}, nil
}

func main() {
	r := &Recorder{}
	/*go func() {
		for {
			err := r.runRecord()
			log.Printf("Error recording: %v", err)
			time.Sleep(time.Second * 5)
		}
	}()*/

	s := &Server{r: r}

	s.processFiles(*procDir)
	if true {
		return
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	pb.RegisterRecordGetterServer(grpcServer, s)
	grpcServer.Serve(lis)
}
