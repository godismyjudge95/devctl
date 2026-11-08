package inspector

import "testing"

func TestNormalize(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"select * from `users` where `users`.`id` = ? limit 1", "select * from `users` where `users`.`id` = ? limit ?"},
		{"SELECT *\n  FROM users -- trailing\nWHERE id = 42 /* c */ AND name = 'O''Brien'", "SELECT * FROM users WHERE id = ? AND name = ?"},
		{"select * from posts where user_id in (1, 2, 3)", "select * from posts where user_id in (?)"},
		{"select * from posts where user_id IN (?,?)", "select * from posts where user_id IN (?)"},
		{"insert into t (a, b) values (1, 'x'), (2, 'y')", "insert into t (a, b) values (?, ?)"},
		{"insert into t (a, b) values (?, ?),(?, ?),(?, ?)", "insert into t (a, b) values (?, ?)"},
		{"select concat(1, 2)", "select concat(?, ?)"},
		{"select * from a join (select 1) b", "select * from a join (select ?) b"},
		{"select id::text from users where id = $1 and name = :name", "select id::text from users where id = ? and name = ?"},
		{"select $body$ it's 1 $body$ as x, $$y$$", "select ? as x, ?"},
		{"select col1, t2.c3 from t4 limit 10 offset 0.5e3", "select col1, t2.c3 from t4 limit ? offset ?"},
		{"select 0x1F", "select ?"},
		{"select data #> '{a}' from t # note", "select data #> ? from t"},
		{`select 'a\'b', "x y"`, `select ?, "x y"`},
		{"  select\t\t1  ", "select ?"},
		{"select -1", "select -?"},
	} {
		if got := Normalize(tc.in); got != tc.want {
			t.Errorf("Normalize(%q)\n got  %q\n want %q", tc.in, got, tc.want)
		}
	}
}

// Eager loads with integer keys inline the ids, so lists of any length from
// one caller must group together.
func TestNormalize_InListsOfAnyLengthMatch(t *testing.T) {
	a := Normalize("select * from posts where posts.user_id in (1, 2, 3)")
	b := Normalize("select * from posts where posts.user_id in (4, 5)")
	if a != b {
		t.Errorf("%q != %q", a, b)
	}
}
