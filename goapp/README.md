```
To deploy:
$ cp -r ~/Documents/future-flow/gitOps/experiment-12-source-blobber/ ~/Documents/source-blobber
$ cd ~/Documents/source-blobber
$ git commit -am "update"
$ git push

To run:
cd ~/Documents/source-blobber
go run main.go
curl "http://localhost:8080/github-org/chap/rails-8-0" --output app.tar.gz

curl -X POST http://localhost:8080/ \
-H "Content-Type: application/json" \
-d '{
  "path": "./app/",
  "repoURL": "https://github.com/chap/rails-8-0",
  "targetRevision": "main"
}'  --output app-goapp.tar.gz



https://source-blobber-e9c8c3638e4b.herokuapp.com/chap/rails-8-0/main