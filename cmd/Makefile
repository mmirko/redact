SHELL := /bin/bash
.PHONY: all
all:
	@for i in `ls | grep -v Makefile` ; do cd ./$$i ; pwd ; CGO_ENABLED=0 go install ; cd .. ; done

