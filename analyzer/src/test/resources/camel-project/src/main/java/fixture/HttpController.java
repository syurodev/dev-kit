package fixture;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RestController;
@RestController public class HttpController {
 private Business business;
 @GetMapping("/http") public void http() { business.literal(); }
}
