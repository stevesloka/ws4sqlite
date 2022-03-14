build-prepare:
	make cleanup
	mkdir bin

cleanup:
	- rm -r bin
	- rm src/ws4sqlite

build:
	make build-prepare
	cd src; CGO_ENABLED=1 go build -a -tags netgo,osusergo,sqlite_omit_load_extension -ldflags '-w -extldflags "-static"' -o ws4sqlite
	mv src/ws4sqlite bin/

zbuild:
	make build
	cd bin; 7zr a -mx9 -t7z ws4sqlite-v0.11.2-`uname -s|tr '[:upper:]' '[:lower:]'`-`uname -m`.7z ws4sqlite

build-nostatic:
	make build-prepare
	cd src; go build -o ws4sqlite
	mv src/ws4sqlite bin/

zbuild-nostatic:
	make build-nostatic
	cd bin; 7zr a -mx9 -t7z ws4sqlite-v0.11.2-`uname -s|tr '[:upper:]' '[:lower:]'`-`uname -m`.7z ws4sqlite

do-test:
	cd src; go test -v -timeout 5m

docker:
	sudo docker build -t local_ws4sqlite:latest .

docker-publish:
	make docker
	sudo docker image tag local_ws4sqlite:latest germanorizzo/ws4sqlite:latest
	sudo docker image tag local_ws4sqlite:latest germanorizzo/ws4sqlite:v0.11.2
	sudo docker push germanorizzo/ws4sqlite:latest
	sudo docker push germanorizzo/ws4sqlite:v0.11.2
	sudo docker rmi local_ws4sqlite:latest
	sudo docker rmi germanorizzo/ws4sqlite:latest
	sudo docker rmi germanorizzo/ws4sqlite:v0.11.2

docker-publish-arm:
	make docker
	sudo docker image tag local_ws4sqlite:latest germanorizzo/ws4sqlite:latest-arm
	sudo docker image tag local_ws4sqlite:latest germanorizzo/ws4sqlite:v0.11.2-arm
	sudo docker push germanorizzo/ws4sqlite:latest-arm
	sudo docker push germanorizzo/ws4sqlite:v0.11.2-arm
	sudo docker rmi local_ws4sqlite:latest
	sudo docker rmi germanorizzo/ws4sqlite:latest-arm
	sudo docker rmi germanorizzo/ws4sqlite:v0.11.2-arm

docker-publish-arm64:
	make docker
	sudo docker image tag local_ws4sqlite:latest germanorizzo/ws4sqlite:latest-arm64
	sudo docker image tag local_ws4sqlite:latest germanorizzo/ws4sqlite:v0.11.2-arm64
	sudo docker push germanorizzo/ws4sqlite:latest-arm64
	sudo docker push germanorizzo/ws4sqlite:v0.11.2-arm64
	sudo docker rmi local_ws4sqlite:latest
	sudo docker rmi germanorizzo/ws4sqlite:latest-arm64
	sudo docker rmi germanorizzo/ws4sqlite:v0.11.2-arm64
