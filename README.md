## Introduction
Ticket is a simple command tool used to query tickets from kyfw.12306.cn, should be easy to use and brief to understand.

## Install
```bash
go get -u github.com/jaysinco/ticket
```
## Usage
```bash
ticket -h
Usage: ticket [from] [to] [YYMMDD]
  -from string
      departure station, can be regexp of code or name or pingyin
  -to string
      arrival station, same as 'from'
  -date string
      should be within sale range and be form of 'YYMMDD'             
```