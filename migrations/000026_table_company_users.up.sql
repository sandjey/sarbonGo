--
-- PostgreSQL database dump
--

\restrict h23ctr6C5eemumffZRyBWd1rDCWoKAtBxYzsUhZurOKGkmF1DviqV79de0Rf2UL

-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: company_users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.company_users (
    id uuid DEFAULT public.uuid_generate_v4() CONSTRAINT app_users_id_not_null NOT NULL,
    phone character varying(20),
    password_hash character varying(255) CONSTRAINT app_users_password_hash_not_null NOT NULL,
    first_name character varying(100),
    last_name character varying(100),
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    company_id uuid,
    role character varying(50)
);


--
-- Name: company_users app_users_phone_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.company_users
    ADD CONSTRAINT app_users_phone_key UNIQUE (phone);


--
-- Name: company_users app_users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.company_users
    ADD CONSTRAINT app_users_pkey PRIMARY KEY (id);


--
-- Name: idx_app_users_phone; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_app_users_phone ON public.company_users USING btree (phone) WHERE (phone IS NOT NULL);


--
-- Name: idx_company_users_company_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_company_users_company_id ON public.company_users USING btree (company_id) WHERE (company_id IS NOT NULL);


--
-- Name: idx_company_users_phone; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_company_users_phone ON public.company_users USING btree (phone) WHERE (phone IS NOT NULL);


--
-- Name: company_users fk_company_users_company; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.company_users
    ADD CONSTRAINT fk_company_users_company FOREIGN KEY (company_id) REFERENCES public.companies(id);


--
-- PostgreSQL database dump complete
--

\unrestrict h23ctr6C5eemumffZRyBWd1rDCWoKAtBxYzsUhZurOKGkmF1DviqV79de0Rf2UL

