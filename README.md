# Plan

Vinyl Recorder is a continous audio recorder and cheap segmenter that you run on a dedicated channel 
of your audio setup. Thus the recoreder is *always* recording whatever is going out of your amplifier.
You can then send it a signal (or have the signal sent to it automatically) to start a new recording, 
at which point it will stop the current recording, chop up by silence and spit out somewhere as a set
of individual tracks.

So the goal of the recorder is two fold:

1. On startup, start recording from the last record we decided to record
  1. If there is no such record wait for a signal to start recording
1. On receiving a new signal stop the current recording (if there is one) and move
   the existing file into the processing folder
1. Additionally start a new recording with the supplied details (discogs id)

And this just keeps continuing

The processor kicks off when we do a copy into the folder, it looks at each file - segments it into tracks,
converts those tracks into individual flac files and then moves the flac files into a folder on your
audio server (whereever that is). Once we moved the tracks into the folder we can trigger a gramophile
update to inform the system that we have ripped this record.

## Dependencies

The following system packages are required:

- `sox`: For audio processing and track splitting.
- `flac`: For converting recordings to FLAC format.
- `alsa-utils`: For `arecord`, used to capture audio.
- `psmisc`: For `killall`, used to manage recording processes.
- `golang`: To build the application.
- `protobuf-compiler`: To generate Go code from proto files (optional if using pre-generated files).

On Debian/Ubuntu, you can install these with:
```bash
sudo apt-get update
sudo apt-get install sox flac alsa-utils psmisc golang protobuf-compiler
```

## Systemd Integration

To run the recorder as a background service that starts on boot and restarts automatically:

The service is configured to automatically pull the latest code from git and recompile the application every time it starts or restarts.

1. Build the application (initial build):
   ```bash
   go build
   ```
2. Copy the service file to the systemd directory:
   ```bash
   sudo cp recorder.service /etc/systemd/system/
   ```
3. Reload systemd to recognize the new service:
   ```bash
   sudo systemctl daemon-reload
   ```
4. Enable the service to start on boot:
   ```bash
   sudo systemctl enable recorder
   ```
5. Start the service:
   ```bash
   sudo systemctl start recorder
   ```

You can check the status of the service with:
```bash
systemctl status recorder
```
And view logs with:
```bash
tail -f recorder.log
```
