package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	db, _ := sql.Open("sqlite", "file:"+os.Args[1]+"?mode=ro")
	for _, q := range os.Args[2:] {
		rows, err := db.Query(q)
		if err != nil {
			fmt.Println(err)
			continue
		}
		cols, _ := rows.Columns()
		fmt.Println(cols)
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			rows.Scan(ptrs...)
			for i, v := range vals {
				if b, ok := v.([]byte); ok {
					vals[i] = string(b)
				}
			}
			fmt.Println(vals...)
		}
	}
}
