// Minimal entry point so `ldcli setup` has a Node project to detect and a file
// to inject SDK initialization into during a manual walkthrough.
const express = require('express');

const app = express();

app.get('/', (req, res) => {
  res.send('hello from the ldcli setup sandbox');
});

app.listen(3000, () => {
  console.log('listening on :3000');
});
