package com.devkit.codegraph;

import org.junit.jupiter.api.Test;

import java.util.List;
import java.util.stream.Collectors;

import static org.junit.jupiter.api.Assertions.assertEquals;

class SqlTablesTest {

    /** "read:orders write:audit" — what the statement touches, as one comparable string. */
    private static String touched(String sql) {
        return touched(sql, false);
    }

    private static String touched(String sql, boolean jpql) {
        return SqlTables.parse(sql, jpql).stream()
                .map(r -> r.access() + ":" + (r.schema().isEmpty() ? "" : r.schema() + ".") + r.name())
                .collect(Collectors.joining(" "));
    }

    @Test
    void selectReadsItsFromAndJoins() {
        assertEquals("read:orders", touched("select * from orders where id = ?"));
        assertEquals("read:orders read:customers read:items",
                touched("SELECT o.id FROM orders o INNER JOIN customers c ON c.id = o.cid LEFT OUTER JOIN items AS i ON i.oid = o.id"));
        assertEquals("read:public.orders", touched("select * from public.orders"));
        assertEquals("read:a read:b", touched("select * from a x, b y where x.id = y.id"));
    }

    @Test
    void writesFollowTheirClause() {
        assertEquals("write:orders", touched("insert into orders (id, name) values (?, ?)"));
        assertEquals("write:orders", touched("update orders set name = ? where id = ?"));
        assertEquals("write:orders", touched("delete from orders where id = ?"));
        assertEquals("write:orders", touched("truncate table orders"));
        assertEquals("write:orders", touched("TRUNCATE orders"));
    }

    @Test
    void aStatementCanWriteOneTableAndReadAnother() {
        assertEquals("write:archive read:orders", touched("insert into archive select * from orders where old = true"));
        assertEquals("write:orders read:blocked",
                touched("delete from orders where cid in (select cid from blocked)"));
        assertEquals("write:orders read:customers",
                touched("update orders o set o.x = 1 from customers c where c.id = o.cid"));
        assertEquals("write:target read:source",
                touched("merge into target t using source s on (t.id = s.id) when matched then update set t.v = s.v"));
    }

    @Test
    void thingsThatLookLikeAClauseButAreNot() {
        assertEquals("read:orders", touched("select extract(year from created_at) from orders"));
        assertEquals("read:orders", touched("select trim(both ' ' from name) from orders"));
        assertEquals("read:orders", touched("select substring(name from 1 for 3) from orders"));
        assertEquals("read:orders", touched("select * from orders where a is distinct from b"));
        assertEquals("read:orders", touched("select * from orders where id = ? for update"));
        assertEquals("write:counters", touched("insert into counters (k, v) values (?, 1) on duplicate key update v = v + 1"));
        assertEquals("write:counters", touched("insert into counters (k) values (?) on conflict (k) do update set v = 1"));
        assertEquals("read:orders", touched("select * from orders join items using (oid)".replace(" join items using (oid)", "")));
        assertEquals("read:orders read:items", touched("select * from orders join items using (oid)"));
    }

    @Test
    void stringsCommentsAndQuotedNames() {
        assertEquals("read:orders", touched("select 'from secrets' as x from orders -- from hidden\n"));
        assertEquals("read:orders", touched("select /* from hidden */ * from orders"));
        assertEquals("read:orders", touched("select * from \"orders\""));
        assertEquals("read:orders", touched("select * from `orders`"));
        assertEquals("read:orders", touched("select * from [orders]"));
        assertEquals("read:orders", touched("select 'it''s from x' from orders"));
        assertEquals("read:orders", touched("select a::text from orders where id = :id and b = ?1"));
    }

    @Test
    void subqueriesAreScannedAndNameNoTableOfTheirOwn() {
        assertEquals("read:a read:b", touched("select * from (select * from a) x join b on b.id = x.id"));
        assertEquals("read:orders read:blocked",
                touched("select * from orders where cid not in (select cid from blocked)"));
        assertEquals("write:t", touched("insert into t values (1)"));
    }

    @Test
    void aCteIsNotATable() {
        assertEquals("read:orders",
                touched("with recent as (select * from orders where d > ?) select * from recent"));
        assertEquals("read:a read:b",
                touched("with x as (select * from a), y as (select * from b) select * from x join y on 1=1"));
    }

    @Test
    void jpqlNamesAreEntitiesAndAssociationPathsAreNot() {
        assertEquals("read:Organization", touched("select o from Organization o where o.id = :id", true));
        assertEquals("read:Organization", touched("select o from Organization o join o.apis a where a.id = :a", true));
        assertEquals("read:Organization read:Api",
                touched("select o from Organization o join Api a on a.org = o", true));
        assertEquals("write:Organization", touched("update Organization o set o.name = :n where o.id = :id", true));
        assertEquals("write:Organization", touched("delete from Organization o where o.id = :id", true));
        assertEquals("write:Organization", touched("delete Organization o where o.id = :id", true));
        assertEquals("read:Organization", touched("select o from com.acme.Organization o", true));
        assertEquals("read:Organization read:Api",
                touched("select o from Organization o left join fetch o.apis join Api a on a.o = o", true));
    }

    @Test
    void nothingToFind() {
        assertEquals("", touched("select 1"));
        assertEquals("", touched(""));
        assertEquals("", touched("call refresh_all()"));
    }

    @Test
    void ddlWritesToo() {
        assertEquals("write:t", touched("create table if not exists t (id int)"));
        assertEquals("write:t", touched("drop table t"));
        assertEquals("write:t", touched("alter table t add column c int"));
    }

    @Test
    void tokensKeepNamesAndDropLiteralsAndParameters() {
        List<String> tokens = SqlTables.tokens("select a, 'x', 12, :p, ?1 from \"T\"");
        assertEquals(List.of("select", "a", ",", "?", ",", "?", ",", "?", ",", "?", "from", "T"), tokens);
    }
}
