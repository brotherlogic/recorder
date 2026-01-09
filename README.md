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
