package com.tst.mwswiki.controller;

import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/api/v1")
public class BasicController {

    @GetMapping("/health")
    public ResponseEntity<String> getHealth() {
        return new ResponseEntity<String>("We're all set!", HttpStatus.OK);
    }
}
