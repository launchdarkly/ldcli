# Recording a clip

Only record when you tested something you can see: the dev-server UI, or ldcli
running in a real terminal. For every other check, write "Video: none, nothing
on screen was tested" in the report.

1. Finish setup before you start recording. Nobody needs to watch an install or
   a build.
2. Start recording right before the check, run one short flow, and stop on the
   screen that shows the result.
3. If the recording failed because of your setup, throw it away, fix the setup,
   and record again. If it shows ldcli failing, keep it: that's evidence for
   the hold.
4. Watch the clip before you link it, and make sure it shows what the report
   says it shows.
5. Name the file after what the clip shows, for example
   `dev-server-ui-pages-after-react-upgrade.mp4`.
