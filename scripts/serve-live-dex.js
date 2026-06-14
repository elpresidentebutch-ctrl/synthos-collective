const http = require("http");
const fs = require("fs");
const path = require("path");

const root = path.resolve(__dirname, "..");
const port = Number(process.env.SYNTHOS_DEX_PORT || 8899);

const server = http.createServer((req, res) => {
  const url = new URL(req.url, `http://127.0.0.1:${port}`);
  const pathname = url.pathname === "/" ? "/THE_COLLECTIVE_DEX_LIVE.html" : url.pathname;
  const file = path.normalize(path.join(root, pathname));

  if (!file.startsWith(root)) {
    res.writeHead(403);
    res.end("forbidden");
    return;
  }

  fs.readFile(file, (err, body) => {
    if (err) {
      res.writeHead(404);
      res.end("not found");
      return;
    }
    const ext = path.extname(file);
    const type = ext === ".html" ? "text/html; charset=utf-8" : "text/plain; charset=utf-8";
    res.writeHead(200, {
      "Content-Type": type,
      "Cache-Control": "no-store",
    });
    res.end(body);
  });
});

server.listen(port, "127.0.0.1", () => {
  console.log(`SYNTHOS live DEX page: http://127.0.0.1:${port}/`);
});
